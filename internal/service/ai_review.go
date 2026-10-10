package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/ai"
	"github.com/forgemill/forgemill/internal/db/models"
)

// Review of an action: deterministic lint always, the model's findings when
// AI assistance is on. The two are merged into one answer so the editor's
// Check panel looks the same either way.

// ActionReviewInput is what the editor has in its form.
type ActionReviewInput struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Script      string                   `json:"script"`
	Parameters  []models.ActionParameter `json:"parameters"`
	Platform    string                   `json:"platform"` // linux | windows | any
	// ActionID: store the review on this action when set (the editor passes
	// it for saved actions and drafts).
	ActionID *int64 `json:"action_id,omitempty"`
}

// ActionReview is the Check panel's content.
type ActionReview struct {
	Summary             string                   `json:"summary,omitempty"`
	Risk                string                   `json:"risk"` // low | medium | high | critical
	Idempotent          *bool                    `json:"idempotent,omitempty"`
	DistroSupport       DistroSupport            `json:"distro_support"`
	Findings            []Finding                `json:"findings"`
	SuggestedParameters []models.ActionParameter `json:"suggested_parameters,omitempty"`
	LintOnly            bool                     `json:"lint_only"` // AI off or failed: lint findings only
	// ScriptHash identifies the script this review is about, so a stored
	// review can be recognised as stale once the script is edited.
	ScriptHash string           `json:"script_hash,omitempty"`
	ReviewedAt string           `json:"reviewed_at,omitempty"`
	AIError    string           `json:"ai_error,omitempty"`
	Model      string           `json:"model,omitempty"`
	Redaction  *ai.RedactReport `json:"redaction,omitempty"`
	DurationMs int64            `json:"duration_ms,omitempty"`
}

// ErrAIInput wraps a rejected request (400).
var ErrAIInput = errors.New("invalid input")

func validateReviewInput(in *ActionReviewInput) error {
	in.Script = strings.ReplaceAll(in.Script, "\r\n", "\n")
	if strings.TrimSpace(in.Script) == "" {
		return fmt.Errorf("%w: script is empty", ErrAIInput)
	}
	if len(in.Script) > ai.MaxInputBytes {
		return fmt.Errorf("%w: script exceeds %d KB", ErrAIInput, ai.MaxInputBytes/1024)
	}
	if len(in.Name) > 200 || len(in.Description) > 2000 {
		return fmt.Errorf("%w: name or description too long", ErrAIInput)
	}
	if len(in.Parameters) > 50 {
		return fmt.Errorf("%w: too many parameters", ErrAIInput)
	}
	return nil
}

// LintOnly runs the deterministic checks and nothing else.
func (s *AIAssistService) LintOnly(in ActionReviewInput) (*ActionReview, error) {
	if err := validateReviewInput(&in); err != nil {
		return nil, err
	}
	findings, distro := LintAction(in.Script, in.Parameters)
	return &ActionReview{Risk: riskFromFindings(findings), DistroSupport: distro, Findings: findings, LintOnly: true, ScriptHash: ScriptHash(in.Script), ReviewedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}

// ScriptHash is the short fingerprint stored with a review.
func ScriptHash(script string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(script, "\r\n", "\n")))
	return hex.EncodeToString(sum[:8])
}

// attachReview stores a review on an action when the input names one.
func (s *AIAssistService) attachReview(in ActionReviewInput, review *ActionReview) {
	if in.ActionID == nil || review == nil {
		return
	}
	raw, err := json.Marshal(review)
	if err != nil {
		return
	}
	if err := s.db.SetActionReview(*in.ActionID, raw); err != nil {
		slog.Warn("could not store action review", "action_id", *in.ActionID, "error", err)
	}
}

// modelReview is the JSON shape the model is asked for.
type modelReview struct {
	Summary       string `json:"summary"`
	Risk          string `json:"risk"`
	Idempotent    *bool  `json:"idempotent"`
	DistroSupport struct {
		Debian bool   `json:"debian"`
		RHEL   bool   `json:"rhel"`
		Notes  string `json:"notes"`
	} `json:"distro_support"`
	Findings []struct {
		Severity   string `json:"severity"`
		Line       int    `json:"line"`
		Title      string `json:"title"`
		Detail     string `json:"detail"`
		Suggestion string `json:"suggestion"`
	} `json:"findings"`
	SuggestedParameters []models.ActionParameter `json:"suggested_parameters"`
}

// ReviewAction lints, then — if AI is on — asks the model and merges. A model
// failure never fails the review: the lint result comes back with AIError.
func (s *AIAssistService) ReviewAction(ctx context.Context, in ActionReviewInput, actor string, actorID *int64) (*ActionReview, error) {
	review, err := s.LintOnly(in)
	if err != nil {
		return nil, err
	}
	defer func() { s.attachReview(in, review) }()
	p, cfg, err := s.provider()
	if err != nil {
		if errors.Is(err, ai.ErrNotConfigured) {
			return review, nil // lint only, by design
		}
		review.AIError = err.Error()
		return review, nil
	}
	if !s.limiter.Allow() {
		review.AIError = ErrAIRateLimited.Error()
		return review, nil
	}
	start := time.Now()
	user, report := buildReviewPrompt(in, review.Findings, cfg)
	review.Redaction = &report
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	var parsed modelReview
	resp, err := completeJSON(ctx, p, ai.Request{System: reviewSystemPrompt, User: user, MaxTokens: 6000, Temperature: 0.1, JSON: true}, &parsed)
	review.DurationMs = time.Since(start).Milliseconds()
	s.auditAI(actor, actorID, "ai.action.review", cfg, resp, report, err, review.DurationMs, len(in.Script))
	if err != nil {
		review.AIError = userFacingAIError(err)
		return review, nil
	}
	review.LintOnly = false
	review.Model = resp.Model
	review.Summary = strings.TrimSpace(parsed.Summary)
	review.Idempotent = parsed.Idempotent
	if parsed.DistroSupport.Notes != "" || !parsed.DistroSupport.Debian || !parsed.DistroSupport.RHEL {
		review.DistroSupport = DistroSupport{Debian: parsed.DistroSupport.Debian, RHEL: parsed.DistroSupport.RHEL, Notes: firstNonEmpty(parsed.DistroSupport.Notes, review.DistroSupport.Notes)}
	}
	for _, f := range parsed.Findings {
		sev := strings.ToLower(strings.TrimSpace(f.Severity))
		if _, ok := severityRank[sev]; !ok {
			sev = "medium"
		}
		if strings.TrimSpace(f.Title) == "" {
			continue
		}
		review.Findings = append(review.Findings, Finding{Severity: sev, Source: "model", Line: f.Line, Title: strings.TrimSpace(f.Title), Detail: strings.TrimSpace(f.Detail), Suggestion: strings.TrimSpace(f.Suggestion)})
	}
	review.SuggestedParameters = sanitizeSuggestedParameters(parsed.SuggestedParameters, in.Parameters)
	sortFindings(review.Findings)
	risk := riskFromFindings(review.Findings)
	if mr := strings.ToLower(parsed.Risk); severityRank[mr] > severityRank[risk] && mr != "info" {
		risk = mr
	}
	review.Risk = risk
	return review, nil
}

func buildReviewPrompt(in ActionReviewInput, lint []Finding, cfg ai.Config) (string, ai.RedactReport) {
	var b strings.Builder
	fmt.Fprintf(&b, "Action name: %s\nDescription: %s\nPlatform: %s\n", strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), firstNonEmpty(in.Platform, "linux"))
	if len(in.Parameters) > 0 {
		b.WriteString("Declared parameters:\n")
		for _, p := range in.Parameters {
			req := ""
			if p.Required {
				req = ", required"
			}
			fmt.Fprintf(&b, "- %s (%s%s): %s\n", p.Name, p.Type, req, strings.TrimSpace(p.Description))
		}
	} else {
		b.WriteString("Declared parameters: none\n")
	}
	if len(lint) > 0 {
		b.WriteString("\nAutomatic findings already reported (do not repeat these):\n")
		for _, f := range lint {
			fmt.Fprintf(&b, "- [%s] line %d: %s\n", f.Severity, f.Line, f.Title)
		}
	}
	b.WriteString("\nScript (line numbers prefixed):\n")
	redacted, report := ai.Redact(in.Script, ai.RedactOptions{Hostnames: cfg.RedactHostnames})
	for i, l := range strings.Split(redacted, "\n") {
		fmt.Fprintf(&b, "%4d  %s\n", i+1, l)
	}
	return b.String(), report
}

var reJSONFence = regexp.MustCompile("(?s)^\\s*```(?:json)?\\s*(.*?)\\s*```\\s*$")

// completeJSON calls the model and parses one JSON object out of the answer,
// tolerating fences and prose around it; on failure it retries once with a
// nudge.
func completeJSON(ctx context.Context, p ai.Provider, req ai.Request, out any) (*ai.Response, error) {
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	if perr := parseJSONObject(resp.Text, out); perr != nil {
		slog.Debug("ai: first answer was not valid JSON, retrying", "error", perr)
		req2 := req
		req2.User = req.User + "\n\nYour previous answer was not valid JSON. Answer again with ONLY the JSON object, no prose, no code fences."
		resp2, err2 := p.Complete(ctx, req2)
		if err2 != nil {
			return nil, err2
		}
		if perr2 := parseJSONObject(resp2.Text, out); perr2 != nil {
			return resp2, fmt.Errorf("the model did not return a usable answer")
		}
		return resp2, nil
	}
	return resp, nil
}

func parseJSONObject(text string, out any) error {
	t := strings.TrimSpace(text)
	if m := reJSONFence.FindStringSubmatch(t); m != nil {
		t = m[1]
	}
	if i := strings.Index(t, "{"); i > 0 {
		t = t[i:]
	}
	if j := strings.LastIndex(t, "}"); j >= 0 && j < len(t)-1 {
		t = t[:j+1]
	}
	dec := json.NewDecoder(strings.NewReader(t))
	return dec.Decode(out)
}

var reParamName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// sanitizeSuggestedParameters keeps only well-formed, new parameters.
func sanitizeSuggestedParameters(suggested, declared []models.ActionParameter) []models.ActionParameter {
	have := map[string]bool{}
	for _, p := range declared {
		have[p.Name] = true
	}
	var out []models.ActionParameter
	for _, p := range suggested {
		p.Name = strings.ToUpper(strings.TrimSpace(p.Name))
		if !reParamName.MatchString(p.Name) || have[p.Name] || len(out) >= 20 {
			continue
		}
		have[p.Name] = true
		switch p.Type {
		case "string", "number", "select", "boolean", "password":
		default:
			p.Type = "string"
		}
		if p.Type == "select" && len(p.Options) == 0 {
			p.Type = "string"
		}
		if strings.TrimSpace(p.Label) == "" {
			p.Label = strings.ReplaceAll(strings.Title(strings.ToLower(strings.ReplaceAll(p.Name, "_", " "))), "  ", " ")
		}
		out = append(out, p)
	}
	return out
}

// userFacingAIError turns provider failures into one line for the panel.
func userFacingAIError(err error) string {
	var pe *ai.ProviderError
	if errors.As(err, &pe) {
		return "The model could not be reached or refused: " + pe.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The model did not answer in time."
	}
	return err.Error()
}

func (s *AIAssistService) auditAI(actor string, actorID *int64, action string, cfg ai.Config, resp *ai.Response, report ai.RedactReport, err error, durationMs int64, inputBytes int) {
	if s.audit == nil {
		return
	}
	meta := map[string]interface{}{
		"provider": cfg.Provider, "model": cfg.Model, "input_bytes": inputBytes, "redacted": report.Total, "redacted_kinds": report.Kinds(), "duration_ms": durationMs, "ok": err == nil,
	}
	if resp != nil {
		meta["input_tokens"], meta["output_tokens"] = resp.InputTokens, resp.OutputTokens
		if resp.Model != "" {
			meta["model"] = resp.Model
		}
	}
	if err != nil {
		meta["error"] = userFacingAIError(err)
	}
	s.audit.Log(actor, actorID, action, "action", "", "", meta)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
