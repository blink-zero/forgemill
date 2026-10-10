package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/ai"
	"github.com/forgemill/forgemill/internal/db/models"
)

// Fixing selected findings. Mechanical ones are edited deterministically;
// the rest go to the model with the instruction to change only what the
// selected findings require. The result is re-checked before it is shown,
// and nothing touches the editor until the user applies the diff.

// ActionFixInput is the editor's content plus the findings the user ticked.
type ActionFixInput struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Script      string                   `json:"script"`
	Parameters  []models.ActionParameter `json:"parameters"`
	Platform    string                   `json:"platform"`
	Findings    []Finding                `json:"findings"`
	ActionID    *int64                   `json:"action_id,omitempty"`
}

// ActionFixResult is the proposed script and what changed.
type ActionFixResult struct {
	Script          string                   `json:"script"`
	Parameters      []models.ActionParameter `json:"parameters"`                 // full list after the fix (declared + added)
	ParametersAdded []models.ActionParameter `json:"parameters_added,omitempty"` // what the model declared
	Changes         []FixChange              `json:"changes"`
	Notes           []string                 `json:"notes,omitempty"`
	Review          *ActionReview            `json:"review,omitempty"` // the fixed script, checked again
	Model           string                   `json:"model,omitempty"`
	UsedAI          bool                     `json:"used_ai"`
	Redaction       *ai.RedactReport         `json:"redaction,omitempty"`
	DurationMs      int64                    `json:"duration_ms"`
}

type modelFix struct {
	Script          string                   `json:"script"`
	ParametersAdded []models.ActionParameter `json:"parameters_added"`
	Changes         []struct {
		Title   string `json:"title"`
		Applied bool   `json:"applied"`
		Note    string `json:"note"`
	} `json:"changes"`
	Notes []string `json:"notes"`
}

func validateFixInput(in *ActionFixInput) error {
	in.Script = strings.ReplaceAll(in.Script, "\r\n", "\n")
	if strings.TrimSpace(in.Script) == "" {
		return fmt.Errorf("%w: script is empty", ErrAIInput)
	}
	if len(in.Script) > ai.MaxInputBytes {
		return fmt.Errorf("%w: script exceeds %d KB", ErrAIInput, ai.MaxInputBytes/1024)
	}
	if len(in.Findings) == 0 {
		return fmt.Errorf("%w: select at least one finding to fix", ErrAIInput)
	}
	if len(in.Findings) > 40 {
		return fmt.Errorf("%w: too many findings selected", ErrAIInput)
	}
	return nil
}

// AutoFixOnly applies the deterministic fixes and re-lints. Works with AI off.
func (s *AIAssistService) AutoFixOnly(in ActionFixInput) (*ActionFixResult, error) {
	if err := validateFixInput(&in); err != nil {
		return nil, err
	}
	start := time.Now()
	script, changes, remaining := AutoFix(in.Script, in.Findings)
	for _, f := range remaining {
		changes = append(changes, FixChange{Rule: f.Rule, Title: f.Title, Line: f.Line, By: "auto", Note: "needs AI assistance or a manual edit"})
	}
	review, _ := s.LintOnly(ActionReviewInput{Name: in.Name, Description: in.Description, Script: script, Parameters: in.Parameters, Platform: in.Platform})
	return &ActionFixResult{Script: script, Parameters: in.Parameters, Changes: changes, Review: review, DurationMs: time.Since(start).Milliseconds()}, nil
}

// FixAction: deterministic fixes first, then the model for what remains,
// then a fresh review of the result.
func (s *AIAssistService) FixAction(ctx context.Context, in ActionFixInput, actor string, actorID *int64, stage func(string)) (*ActionFixResult, error) {
	if stage == nil {
		stage = func(string) {}
	}
	if err := validateFixInput(&in); err != nil {
		return nil, err
	}
	start := time.Now()
	stage("fixing")
	script, changes, remaining := AutoFix(in.Script, in.Findings)
	res := &ActionFixResult{Script: script, Parameters: in.Parameters, Changes: changes}
	if len(remaining) > 0 {
		p, cfg, err := s.provider()
		if err != nil {
			return nil, err
		}
		if !s.limiter.Allow() {
			return nil, ErrAIRateLimited
		}
		user, report := buildFixPrompt(in, script, remaining, cfg)
		res.Redaction = &report
		ctx2, cancel := context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
		var parsed modelFix
		resp, err := completeJSON(ctx2, p, ai.Request{System: fixSystemPrompt, User: user, MaxTokens: 24000, Temperature: 0.1, JSON: true, Schema: fixSchema}, &parsed)
		s.auditAI(actor, actorID, "ai.action.fix", cfg, resp, report, err, time.Since(start).Milliseconds(), len(in.Script))
		if err != nil {
			return nil, err
		}
		fixed := strings.TrimSpace(strings.ReplaceAll(parsed.Script, "\r\n", "\n"))
		if fixed == "" {
			return nil, errors.New("the model returned an empty script")
		}
		if err := ValidateActionScript(fixed + "\n"); err != nil {
			return nil, fmt.Errorf("the model's script is not acceptable: %w", err)
		}
		res.Script = fixed + "\n"
		res.UsedAI = true
		res.Model = resp.Model
		res.ParametersAdded = sanitizeSuggestedParameters(parsed.ParametersAdded, in.Parameters)
		res.Parameters = append(append([]models.ActionParameter{}, in.Parameters...), res.ParametersAdded...)
		res.Notes = clipAll(parsed.Notes, 300, 8)
		reported := map[string]bool{}
		for _, c := range parsed.Changes {
			t := strings.TrimSpace(c.Title)
			if t == "" {
				continue
			}
			reported[strings.ToLower(t)] = true
			res.Changes = append(res.Changes, FixChange{Title: t, Applied: c.Applied, By: "model", Note: strings.TrimSpace(c.Note)})
		}
		for _, f := range remaining {
			if !reported[strings.ToLower(f.Title)] {
				res.Changes = append(res.Changes, FixChange{Rule: f.Rule, Title: f.Title, Line: f.Line, By: "model", Note: "the model did not say whether it addressed this; check the diff"})
			}
		}
	}
	stage("reviewing")
	review, rerr := s.ReviewAction(ctx, ActionReviewInput{Name: in.Name, Description: in.Description, Script: res.Script, Parameters: res.Parameters, Platform: in.Platform}, actor, actorID)
	if rerr == nil {
		res.Review = review
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return res, nil
}

func buildFixPrompt(in ActionFixInput, script string, findings []Finding, cfg ai.Config) (string, ai.RedactReport) {
	var b strings.Builder
	fmt.Fprintf(&b, "Action name: %s\nDescription: %s\nPlatform: %s\n", strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), firstNonEmpty(in.Platform, "linux"))
	if len(in.Parameters) > 0 {
		b.WriteString("Declared parameters (available as environment variables):\n")
		for _, p := range in.Parameters {
			fmt.Fprintf(&b, "- %s (%s): %s\n", p.Name, p.Type, strings.TrimSpace(p.Description))
		}
	}
	b.WriteString("\nFix ONLY these findings (each: severity, line, title — detail — suggested fix):\n")
	for i, f := range findings {
		fmt.Fprintf(&b, "%d. [%s] line %d: %s — %s — %s\n", i+1, f.Severity, f.Line, f.Title, f.Detail, f.Suggestion)
	}
	b.WriteString("\nScript (line numbers prefixed; return the script WITHOUT the numbers):\n")
	redacted, report := ai.Redact(script, ai.RedactOptions{Hostnames: cfg.RedactHostnames})
	for i, l := range strings.Split(redacted, "\n") {
		fmt.Fprintf(&b, "%4d  %s\n", i+1, l)
	}
	return b.String(), report
}

// StartFixJob runs FixAction in the background.
func (s *AIAssistService) StartFixJob(in ActionFixInput, actor string, actorID *int64) (*AIJob, error) {
	if err := validateFixInput(&in); err != nil {
		return nil, err
	}
	_, _, remaining := AutoFix(in.Script, in.Findings)
	if len(remaining) > 0 {
		if _, _, err := s.provider(); err != nil {
			return nil, err
		}
	}
	return s.jobs.startFix("fix", actorID, func(stage func(string)) (*ActionFixResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), aiRequestBudget)
		defer cancel()
		return s.FixAction(ctx, in, actor, actorID, stage)
	})
}
