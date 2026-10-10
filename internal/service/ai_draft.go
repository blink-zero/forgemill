package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/ai"
	"github.com/forgemill/forgemill/internal/db/models"
)

// Drafting an action from a description. The draft is validated exactly
// like a user submission, then linted and reviewed, so the editor shows it
// together with its findings. Nothing is saved: "Use this draft" fills the
// form and the user still clicks Create.

// ActionDraftInput is the request from the editor.
type ActionDraftInput struct {
	Prompt string `json:"prompt"`
	// Platform hint: linux (default) | any. Windows is not drafted.
	Platform string `json:"platform,omitempty"`
	// Optional starting point: the user's current script and parameters,
	// when they ask for a change rather than something new.
	ExistingScript     string                   `json:"existing_script,omitempty"`
	ExistingParameters []models.ActionParameter `json:"existing_parameters,omitempty"`
}

// ActionDraft is what comes back: a complete, validated action plus its review.
type ActionDraft struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Category    string                   `json:"category"`
	Script      string                   `json:"script"`
	Parameters  []models.ActionParameter `json:"parameters"`
	Tags        []string                 `json:"tags"`
	Notes       []string                 `json:"notes,omitempty"`
	Warnings    []string                 `json:"warnings,omitempty"`
	Review      *ActionReview            `json:"review,omitempty"`
	Model       string                   `json:"model,omitempty"`
	Redaction   *ai.RedactReport         `json:"redaction,omitempty"`
	DurationMs  int64                    `json:"duration_ms"`
	// Refused is set when the model declined (unsafe/impossible request):
	// Script is empty and Warnings say why.
	Refused bool `json:"refused,omitempty"`
}

type modelDraft struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Category    string                   `json:"category"`
	Script      string                   `json:"script"`
	Parameters  []models.ActionParameter `json:"parameters"`
	Tags        []string                 `json:"tags"`
	Notes       []string                 `json:"notes"`
	Warnings    []string                 `json:"warnings"`
}

var validCategories = map[string]bool{"packages": true, "scripts": true, "security": true, "monitoring": true, "custom": true}

func validateDraftInput(in *ActionDraftInput) error {
	in.Prompt = strings.TrimSpace(in.Prompt)
	if len(in.Prompt) < 8 {
		return fmt.Errorf("%w: describe what the action should do (a sentence or two)", ErrAIInput)
	}
	if len(in.Prompt) > 4000 {
		return fmt.Errorf("%w: description is too long (4000 characters max)", ErrAIInput)
	}
	if len(in.ExistingScript) > ai.MaxInputBytes {
		return fmt.Errorf("%w: existing script exceeds %d KB", ErrAIInput, ai.MaxInputBytes/1024)
	}
	if len(in.ExistingParameters) > 50 {
		return fmt.Errorf("%w: too many parameters", ErrAIInput)
	}
	return nil
}

// DraftAction asks the model for a complete action and returns it validated
// and reviewed. Unlike review, drafting needs AI on: without it there is
// nothing to return, so ErrNotConfigured is returned (409).
func (s *AIAssistService) DraftAction(ctx context.Context, in ActionDraftInput, actor string, actorID *int64) (*ActionDraft, error) {
	return s.draftAction(ctx, in, actor, actorID, nil)
}

// draftAction is DraftAction with a progress hook, used by background jobs
// to report "drafting" / "reviewing".
func (s *AIAssistService) draftAction(ctx context.Context, in ActionDraftInput, actor string, actorID *int64, stage func(string)) (*ActionDraft, error) {
	if stage == nil {
		stage = func(string) {}
	}
	if err := validateDraftInput(&in); err != nil {
		return nil, err
	}
	p, cfg, err := s.provider()
	if err != nil {
		return nil, err
	}
	if !s.limiter.Allow() {
		return nil, ErrAIRateLimited
	}
	start := time.Now()
	user, report := buildDraftPrompt(in, cfg)
	// One budget for the draft call and its review (each may retry once);
	// the handler's deadline is longer than this, so the user always gets
	// an answer or a reason rather than a dropped connection.
	ctx, cancel := context.WithTimeout(ctx, draftTotalBudget(cfg.Timeout))
	defer cancel()
	stage("drafting")
	var parsed modelDraft
	resp, err := completeJSON(ctx, p, ai.Request{System: draftSystemPrompt, User: user, MaxTokens: 8000, Temperature: 0.3, JSON: true}, &parsed)
	duration := time.Since(start).Milliseconds()
	s.auditAI(actor, actorID, "ai.action.draft", cfg, resp, report, err, duration, len(in.Prompt)+len(in.ExistingScript))
	if err != nil {
		return nil, err
	}
	draft := &ActionDraft{
		Name:        clip(strings.TrimSpace(parsed.Name), 60),
		Description: clip(strings.TrimSpace(parsed.Description), 500),
		Category:    strings.ToLower(strings.TrimSpace(parsed.Category)),
		Script:      strings.TrimSpace(strings.ReplaceAll(parsed.Script, "\r\n", "\n")),
		Parameters:  sanitizeDraftParameters(parsed.Parameters),
		Tags:        sanitizeTags(parsed.Tags),
		Notes:       clipAll(parsed.Notes, 300, 8),
		Warnings:    clipAll(parsed.Warnings, 300, 8),
		Model:       resp.Model,
		Redaction:   &report,
		DurationMs:  duration,
	}
	if !validCategories[draft.Category] {
		draft.Category = "custom"
	}
	if draft.Script == "" {
		draft.Refused = true
		if len(draft.Warnings) == 0 {
			draft.Warnings = []string{"The model did not produce a script for this request."}
		}
		return draft, nil
	}
	if draft.Script != "" {
		draft.Script += "\n"
	}
	if err := ValidateActionScript(draft.Script); err != nil {
		return nil, fmt.Errorf("the model's script is not acceptable: %w", err)
	}
	if draft.Name == "" {
		draft.Name = "Untitled action"
	}
	// Every variable the script reads must be a parameter; the linter's
	// undeclared-parameter rule catches misses, and the review below shows
	// them. The draft is reviewed like a user's script would be.
	stage("reviewing")
	review, rerr := s.ReviewAction(ctx, ActionReviewInput{Name: draft.Name, Description: draft.Description, Script: draft.Script, Parameters: draft.Parameters, Platform: firstNonEmpty(in.Platform, "linux")}, actor, actorID)
	if rerr == nil {
		draft.Review = review
	}
	return draft, nil
}

func buildDraftPrompt(in ActionDraftInput, cfg ai.Config) (string, ai.RedactReport) {
	var b strings.Builder
	fmt.Fprintf(&b, "Target platform: %s\n\nRequest:\n%s\n", firstNonEmpty(in.Platform, "linux"), in.Prompt)
	var report ai.RedactReport
	if strings.TrimSpace(in.ExistingScript) != "" {
		redacted, rep := ai.Redact(in.ExistingScript, ai.RedactOptions{Hostnames: cfg.RedactHostnames})
		report = rep
		b.WriteString("\nStart from this existing script (keep what is right, change what the request asks for):\n")
		b.WriteString(redacted)
		b.WriteString("\n")
		if len(in.ExistingParameters) > 0 {
			b.WriteString("Its declared parameters:\n")
			for _, p := range in.ExistingParameters {
				fmt.Fprintf(&b, "- %s (%s): %s\n", p.Name, p.Type, strings.TrimSpace(p.Description))
			}
		}
	}
	return b.String(), report
}

var reTag = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,29}$`)

func sanitizeTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] || !reTag.MatchString(t) || len(out) >= 10 {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// sanitizeDraftParameters makes the model's parameter list acceptable to
// the same validation a user's submission gets.
func sanitizeDraftParameters(params []models.ActionParameter) []models.ActionParameter {
	out := sanitizeSuggestedParameters(params, nil)
	for i := range out {
		out[i].Label = clip(out[i].Label, 60)
		out[i].Description = clip(out[i].Description, 200)
		out[i].Default = clip(out[i].Default, 200)
		out[i].Placeholder = clip(out[i].Placeholder, 100)
		if out[i].Type != "select" {
			out[i].Options = nil
		}
	}
	if out == nil {
		out = []models.ActionParameter{}
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func clipAll(items []string, n, max int) []string {
	var out []string
	for _, it := range items {
		if t := strings.TrimSpace(it); t != "" && len(out) < max {
			out = append(out, clip(t, n))
		}
	}
	return out
}

// draftTotalBudget: the draft call, one retry, and the review that follows.
func draftTotalBudget(perCall time.Duration) time.Duration {
	if perCall <= 0 {
		perCall = ai.DefaultTimeout
	}
	return 3 * perCall
}
