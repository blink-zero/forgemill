package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// ModelInfo is one model the provider offers.
type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ModelLister is implemented by providers that can enumerate their models.
type ModelLister interface {
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// ListModels: GET /v1/models on the Messages API.
func (a *anthropic) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.effectiveBaseURL()+"/v1/models?limit=100", nil)
	if err != nil {
		return nil, err
	}
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
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &ProviderError{Provider: "Anthropic", Status: resp.StatusCode, Message: "unexpected response shape"}
	}
	models := make([]ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		models = append(models, ModelInfo{ID: m.ID, Name: m.DisplayName})
	}
	return sortModels(models), nil
}

// ListModels: GET /models on an OpenAI-compatible endpoint (Ollama's /v1
// included). Models that are plainly not chat models are left out.
func (o *openAI) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.cfg.effectiveBaseURL()+"/models", nil)
	if err != nil {
		return nil, err
	}
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
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &ProviderError{Provider: "OpenAI-compatible endpoint", Status: resp.StatusCode, Message: "unexpected response shape"}
	}
	models := make([]ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID == "" || isNonChatModel(m.ID) {
			continue
		}
		models = append(models, ModelInfo{ID: m.ID})
	}
	return sortModels(models), nil
}

var nonChatMarkers = []string{"embedding", "embed-", "whisper", "tts", "dall-e", "moderation", "transcribe", "realtime", "audio", "image", "babbage", "davinci-002", "search", "similarity", "rerank"}

func isNonChatModel(id string) bool {
	l := strings.ToLower(id)
	for _, m := range nonChatMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

func sortModels(models []ModelInfo) []ModelInfo {
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}
