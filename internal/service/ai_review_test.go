package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/ai"
	"github.com/forgemill/forgemill/internal/db/models"
)

// fakeAIProvider returns canned answers and records what it was sent.
type fakeAIProvider struct {
	answers []string
	err     error
	calls   []ai.Request
}

func (f *fakeAIProvider) Name() string { return "fake" }
func (f *fakeAIProvider) Complete(_ context.Context, req ai.Request) (*ai.Response, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	a := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return &ai.Response{Text: a, Model: "fake-1", InputTokens: 10, OutputTokens: 5}, nil
}

func enableAI(t *testing.T, a *AIAssistService, fp *fakeAIProvider) {
	t.Helper()
	for k, v := range map[string]string{SettingAIEnabled: "true", SettingAIProvider: ai.ProviderOpenAI, SettingAIModel: "fake-1", SettingAIAllowPrivateEndpoint: "true"} {
		if err := a.db.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	a.newProvider = func(ai.Config) (ai.Provider, error) { return fp, nil }
}

const sampleScript = "#!/bin/bash\napt-get install nginx\nMYSQL_PASSWORD=hunter2\nmysql -u root --password=$MYSQL_PASSWORD -e 'select 1'\n"

func TestReviewActionIsLintOnlyWhenAIOff(t *testing.T) {
	a, _ := newAITestService(t)
	r, err := a.ReviewAction(context.Background(), ActionReviewInput{Name: "x", Script: sampleScript}, "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.LintOnly || r.Model != "" || r.AIError != "" || len(r.Findings) == 0 || r.Risk != "high" {
		t.Fatalf("lint-only review: %+v", r)
	}
	for _, f := range r.Findings {
		if f.Source != "lint" {
			t.Errorf("unexpected source %s", f.Source)
		}
	}
	if _, err := a.ReviewAction(context.Background(), ActionReviewInput{Script: "   "}, "admin", nil); !errors.Is(err, ErrAIInput) {
		t.Errorf("empty script must be rejected, got %v", err)
	}
}

func TestReviewActionMergesModelFindingsAndRedacts(t *testing.T) {
	a, _ := newAITestService(t)
	fp := &fakeAIProvider{answers: []string{"```json\n" + `{"summary":"Installs nginx and runs a query.","risk":"high","idempotent":false,
	  "distro_support":{"debian":true,"rhel":false,"notes":"apt only"},
	  "findings":[{"severity":"medium","line":4,"title":"Query result is discarded","detail":"The select does nothing.","suggestion":"Remove it or use the result."}],
	  "suggested_parameters":[{"name":"mysql_password","type":"password","required":true},{"name":"bad name","type":"string"},{"name":"MYSQL_HOST","label":"MySQL host","type":"weird"}]}` + "\n```"}}
	enableAI(t, a, fp)
	r, err := a.ReviewAction(context.Background(), ActionReviewInput{Name: "db", Script: sampleScript, Parameters: []models.ActionParameter{{Name: "UNUSED", Type: "string"}}}, "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.LintOnly || r.Model != "fake-1" || r.Summary == "" || r.Idempotent == nil || *r.Idempotent {
		t.Fatalf("model review: %+v", r)
	}
	var lint, model int
	for _, f := range r.Findings {
		if f.Source == "lint" {
			lint++
		} else {
			model++
		}
	}
	if lint == 0 || model != 1 {
		t.Errorf("merge: lint=%d model=%d", lint, model)
	}
	if r.Risk != "high" {
		t.Errorf("risk %s", r.Risk)
	}
	// Suggested parameters are sanitised: upper-cased, bad names dropped, unknown type → string, label filled.
	if len(r.SuggestedParameters) != 2 || r.SuggestedParameters[0].Name != "MYSQL_PASSWORD" || r.SuggestedParameters[0].Type != "password" || r.SuggestedParameters[1].Type != "string" || r.SuggestedParameters[0].Label == "" {
		t.Errorf("suggested: %+v", r.SuggestedParameters)
	}
	// The secret never reached the model; structure did.
	sent := fp.calls[0].User
	if strings.Contains(sent, "hunter2") {
		t.Errorf("secret leaked to the model:\n%s", sent)
	}
	if !strings.Contains(sent, "MYSQL_PASSWORD=«REDACTED:password»") || !strings.Contains(sent, "Automatic findings already reported") || !strings.Contains(sent, "   2  apt-get install nginx") {
		t.Errorf("prompt shape:\n%s", sent)
	}
	if r.Redaction == nil || r.Redaction.Total < 1 {
		t.Errorf("redaction report: %+v", r.Redaction)
	}
	if !fp.calls[0].JSON || fp.calls[0].System == "" {
		t.Error("review must ask for JSON with the system prompt")
	}
}

func TestReviewActionSurvivesBadModelAnswersAndProviderErrors(t *testing.T) {
	a, _ := newAITestService(t)
	// Prose first, valid JSON on the retry.
	fp := &fakeAIProvider{answers: []string{"Sure! Here is my review...", `{"summary":"ok","risk":"low","findings":[]}`}}
	enableAI(t, a, fp)
	r, err := a.ReviewAction(context.Background(), ActionReviewInput{Script: "set -e\necho hi\n"}, "admin", nil)
	if err != nil || r.LintOnly || r.Summary != "ok" || len(fp.calls) != 2 || !strings.Contains(fp.calls[1].User, "not valid JSON") {
		t.Fatalf("retry: %+v err %v calls %d", r, err, len(fp.calls))
	}
	// Garbage twice → lint result with an AI error, never a failure.
	fp = &fakeAIProvider{answers: []string{"nope", "still nope"}}
	enableAI(t, a, fp)
	r, err = a.ReviewAction(context.Background(), ActionReviewInput{Script: "set -e\necho hi\n"}, "admin", nil)
	if err != nil || !r.LintOnly || !strings.Contains(r.AIError, "usable answer") {
		t.Fatalf("garbage: %+v err %v", r, err)
	}
	// Provider refused → lint result with the upstream message.
	fp = &fakeAIProvider{err: &ai.ProviderError{Provider: "X", Status: 401, Message: "bad key"}}
	enableAI(t, a, fp)
	r, err = a.ReviewAction(context.Background(), ActionReviewInput{Script: "set -e\necho hi\n"}, "admin", nil)
	if err != nil || !r.LintOnly || !strings.Contains(r.AIError, "bad key") {
		t.Fatalf("provider error: %+v err %v", r, err)
	}
}

// A script embedded with raw newlines inside a JSON string is repaired.
func TestParseJSONObjectRepairsRawNewlinesInStrings(t *testing.T) {
	raw := "Sure!\n```json\n{\"name\": \"x\", \"script\": \"#!/bin/bash\nset -e\necho \\\"hi\\\"\n\"}\n```"
	var out struct {
		Name   string `json:"name"`
		Script string `json:"script"`
	}
	if err := parseJSONObject(raw, &out); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if out.Name != "x" || !strings.HasPrefix(out.Script, "#!/bin/bash\nset -e") || !strings.Contains(out.Script, `echo "hi"`) {
		t.Errorf("parsed %+v", out)
	}
}
