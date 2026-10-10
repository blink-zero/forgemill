package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/ai"
	"github.com/forgemill/forgemill/internal/db/models"
)

const fixScript = "#!/bin/bash\napt-get install nginx\nMYSQL_PASSWORD=hunter2\nmysql --password=$MYSQL_PASSWORD -e 'select 1'\n"

func TestAutoFixOnlyWorksWithAIOff(t *testing.T) {
	a, _ := newAITestService(t)
	findings, _ := LintAction(fixScript, nil)
	res, err := a.AutoFixOnly(ActionFixInput{Script: fixScript, Findings: findings})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Script, "set -euo pipefail") || !strings.Contains(res.Script, "apt-get install -y nginx") || res.UsedAI {
		t.Fatalf("auto result: %+v", res)
	}
	var applied, needsAI int
	for _, c := range res.Changes {
		if c.Applied {
			applied++
		} else if strings.Contains(c.Note, "needs AI") {
			needsAI++
		}
	}
	if applied < 3 || needsAI == 0 {
		t.Errorf("changes: %+v", res.Changes)
	}
	if res.Review == nil || !res.Review.LintOnly {
		t.Errorf("auto fix must re-lint: %+v", res.Review)
	}
	if _, err := a.AutoFixOnly(ActionFixInput{Script: fixScript}); !errors.Is(err, ErrAIInput) {
		t.Errorf("no findings selected must be rejected, got %v", err)
	}
}

func TestFixActionSendsOnlyRemainingFindingsAndMergesModelFix(t *testing.T) {
	a, _ := newAITestService(t)
	fixed := "#!/bin/bash\nset -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\napt-get install -y nginx\n: \"${MYSQL_PASSWORD:?}\"\nmysql --password=\"$MYSQL_PASSWORD\" -e 'select 1'\n"
	fp := &fakeAIProvider{answers: []string{
		`{"script":` + jsonString(fixed) + `,"parameters_added":[{"name":"MYSQL_PASSWORD","label":"MySQL password","type":"password","required":true}],"changes":[{"title":"A secret is written into the script","applied":true,"note":"made MYSQL_PASSWORD a password parameter"}],"notes":["consider --protocol=socket"]}`,
		`{"summary":"ok","risk":"low","idempotent":true,"findings":[]}`,
	}}
	enableAI(t, a, fp)
	findings, _ := LintAction(fixScript, nil)
	var stages []string
	res, err := a.FixAction(context.Background(), ActionFixInput{Name: "db", Script: fixScript, Findings: findings}, "admin", nil, func(s string) { stages = append(stages, s) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.UsedAI || res.Script != fixed || len(res.ParametersAdded) != 1 || res.ParametersAdded[0].Type != "password" || len(res.Parameters) != 1 {
		t.Fatalf("result: %+v", res)
	}
	// The model got the already auto-fixed script and only the remaining findings, redacted.
	sent := fp.calls[0].User
	if strings.Contains(sent, "hunter2") || !strings.Contains(sent, "set -euo pipefail") || strings.Contains(sent, "Script does not stop on the first error") || !strings.Contains(sent, "A secret is written into the script") {
		t.Errorf("prompt:\n%s", sent)
	}
	if fp.calls[0].System != fixSystemPrompt || fp.calls[0].Schema == nil {
		t.Error("fix must use its prompt and schema")
	}
	// Changes: auto ones + the model's, and a fresh review.
	var auto, model int
	for _, c := range res.Changes {
		if c.By == "auto" && c.Applied {
			auto++
		}
		if c.By == "model" && c.Applied {
			model++
		}
	}
	if auto < 3 || model != 1 || res.Review == nil || res.Review.LintOnly || len(res.Notes) != 1 {
		t.Errorf("changes auto=%d model=%d review=%+v notes=%v", auto, model, res.Review, res.Notes)
	}
	if strings.Join(stages, ",") != "fixing,reviewing" {
		t.Errorf("stages %v", stages)
	}
}

func TestFixActionRefusesEmptyOrOversizedModelScript(t *testing.T) {
	a, _ := newAITestService(t)
	fp := &fakeAIProvider{answers: []string{`{"script":"","changes":[]}`}}
	enableAI(t, a, fp)
	findings, _ := LintAction("curl x | sh\n", nil)
	sel := []Finding{}
	for _, f := range findings {
		if f.Rule == "pipe-to-shell" {
			sel = append(sel, f)
		}
	}
	if _, err := a.FixAction(context.Background(), ActionFixInput{Script: "curl x | sh\n", Findings: sel}, "admin", nil, nil); err == nil || !strings.Contains(err.Error(), "empty script") {
		t.Errorf("empty script must be refused, got %v", err)
	}
	// With AI off and only AI-fixable findings selected, the job is refused up front.
	b, _ := newAITestService(t)
	if _, err := b.StartFixJob(ActionFixInput{Script: "curl x | sh\n", Findings: sel}, "admin", nil); !errors.Is(err, ai.ErrNotConfigured) {
		t.Errorf("want ErrNotConfigured, got %v", err)
	}
	// With AI off but only auto-fixable selected, the job runs fine.
	autoOnly := []Finding{}
	all, _ := LintAction(fixScript, nil)
	for _, f := range all {
		if f.Fix == "auto" {
			autoOnly = append(autoOnly, f)
		}
	}
	job, err := b.StartFixJob(ActionFixInput{Script: fixScript, Findings: autoOnly}, "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, b, job.ID, nil)
	if done.Status != "done" || done.Fix == nil || !strings.Contains(done.Fix.Script, "set -euo pipefail") {
		t.Fatalf("auto-only job: %+v", done)
	}
	_ = models.ActionStatusActive
}

func jsonString(s string) string {
	b := strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
