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
		return nil, &ProviderError{Provider: "Anthropic", Message: trimMessage(err.Error())}
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
			Type string `json:"type"`
			Text string `json:"text"`
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
		if c.Type == "text" {
			text.WriteString(c.Text)
		} else {
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
