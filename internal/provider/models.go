package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ModelInfo holds metadata about a model from the provider's /v1/models endpoint.
type ModelInfo struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length"` // OpenRouter format
	ContextWindow int    `json:"context_window"` // OpenAI format
}

// ContextSize returns the context window size from either field.
func (m *ModelInfo) ContextSize() int {
	if m.ContextLength > 0 {
		return m.ContextLength
	}
	return m.ContextWindow
}

// FetchModelInfo queries the provider's /v1/models endpoint for context window size.
// Returns nil if the endpoint is unavailable or the model isn't found.
func FetchModelInfo(baseURL, apiKey, modelID string) *ModelInfo {
	if baseURL == "" || apiKey == "" {
		return nil
	}

	url := strings.TrimSuffix(baseURL, "/") + "/v1/models"
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	// Find exact match or suffix match
	for _, m := range result.Data {
		if m.ID == modelID || strings.HasSuffix(m.ID, "/"+modelID) {
			return &m
		}
	}

	// Try partial match
	for _, m := range result.Data {
		if strings.Contains(m.ID, modelID) {
			return &m
		}
	}

	_ = fmt.Sprintf // avoid unused import
	return nil
}
