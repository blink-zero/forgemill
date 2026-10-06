package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// openAI speaks Chat Completions — OpenAI itself and every compatible server
// (Ollama's /v1, vLLM, LM Studio, OpenRouter, Azure-style gateways).
type openAI struct {
	cfg  Config
	http *http.Client
}

func (o *openAI) Name() string { return ProviderOpenAI }

func (o *openAI) Complete(ctx context.Context, req Request) (*Response, error) {
	messages := []map[string]string{}
	if req.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.System})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.User})
	body := map[string]any{
		"model":    o.cfg.Model,
		"messages": messages,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.effectiveBaseURL()+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	}
	resp, err := o.http.Do(httpReq)
	if err != nil {
		return nil, &ProviderError{Provider: "OpenAI-compatible endpoint", Message: trimMessage(err.Error())}
	}
	defer resp.Body.Close()
	raw, err := readBody(resp.Body)
	if err != nil {
		return nil, &ProviderError{Provider: "OpenAI-compatible endpoint", Status: resp.StatusCode, Message: "could not read response"}
	}
	if resp.StatusCode/100 != 2 {
		return nil, &ProviderError{Provider: "OpenAI-compatible endpoint", Status: resp.StatusCode, Message: apiErrorMessage(raw)}
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return nil, &ProviderError{Provider: "OpenAI-compatible endpoint", Status: resp.StatusCode, Message: "unexpected response shape"}
	}
	if out.Choices[0].Message.Content == "" {
		return nil, &ProviderError{Provider: "OpenAI-compatible endpoint", Status: resp.StatusCode, Message: "empty answer"}
	}
	return &Response{Text: out.Choices[0].Message.Content, Model: out.Model, InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}, nil
}
