package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// anthropic speaks the Messages API.
type anthropic struct {
	cfg  Config
	http *http.Client
}

func (a *anthropic) Name() string { return ProviderAnthropic }

const anthropicVersion = "2023-06-01"

// answerToolName is the forced tool that carries a structured answer.
const answerToolName = "answer"

func (a *anthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	body := map[string]any{
		"model":      a.cfg.Model,
		"max_tokens": maxTokens,
		"messages":   []map[string]any{{"role": "user", "content": req.User}},
	}
	if req.System != "" {
		body["system"] = req.System
	}
	// Structured answers go through a forced tool call: the API validates
	// the tool input as JSON, so scripts full of quotes and newlines come
	// back intact instead of as a string the model had to escape by hand.
	if req.JSON {
		schema := req.Schema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		body["tools"] = []map[string]any{{
			"name":         answerToolName,
			"description":  "Return your answer as this structured object. Call it exactly once.",
			"input_schema": schema,
		}}
		body["tool_choice"] = map[string]any{"type": "tool", "name": answerToolName}
	}
	// No temperature: the current Claude models reject the parameter
	// ("`temperature` is deprecated for this model") and the default is
	// right for review and drafting anyway. Request.Temperature is honoured
	// by the OpenAI-compatible adapter only.
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.effectiveBaseURL()+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, &ProviderError{Provider: "Anthropic", Message: transportMessage(err, a.cfg.timeout())}
	}
	defer resp.Body.Close()
	raw, err := readBody(resp.Body)
	if err != nil {
		return nil, &ProviderError{Provider: "Anthropic", Status: resp.StatusCode, Message: "could not read response"}
	}
	if resp.StatusCode/100 != 2 {
		return nil, &ProviderError{Provider: "Anthropic", Status: resp.StatusCode, Message: apiErrorMessage(raw)}
	}
	var out struct {
		Model   string `json:"model"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &ProviderError{Provider: "Anthropic", Status: resp.StatusCode, Message: "unexpected response shape"}
	}
	var text strings.Builder
	var otherBlocks []string
	for _, c := range out.Content {
		switch {
		case c.Type == "tool_use" && c.Name == answerToolName && len(c.Input) > 0:
			// The structured answer; it wins over any surrounding prose.
			text.Reset()
			text.Write(c.Input)
			otherBlocks = nil
		case c.Type == "text" && text.Len() == 0:
			text.WriteString(c.Text)
		case c.Type == "text":
		default:
			otherBlocks = append(otherBlocks, c.Type)
		}
	}
	if text.Len() == 0 {
		// Models that reason before answering can spend the whole output
		// budget without producing text; say so instead of "empty answer".
		msg := "empty answer"
		if out.StopReason == "max_tokens" {
			msg = fmt.Sprintf("the model used its whole output budget (%d tokens) without producing an answer; raise max_tokens", maxTokens)
		} else if len(otherBlocks) > 0 {
			msg = fmt.Sprintf("the model returned no text (only %s blocks)", strings.Join(otherBlocks, ", "))
		}
		return nil, &ProviderError{Provider: "Anthropic", Status: resp.StatusCode, Message: msg}
	}
	return &Response{Text: text.String(), Model: out.Model, InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}, nil
}

// apiErrorMessage digs the human message out of either API's error body.
func apiErrorMessage(raw []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(raw, &e) == nil {
		switch {
		case e.Error.Message != "":
			return trimMessage(e.Error.Message)
		case e.Message != "":
			return trimMessage(e.Message)
		case e.Detail != "":
			return trimMessage(e.Detail)
		}
	}
	if s := strings.TrimSpace(string(raw)); s != "" {
		return trimMessage(s)
	}
	return fmt.Sprintf("request failed")
}
