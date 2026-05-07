package provider

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
//
// URL construction handles both styles of baseURL:
//   - "https://api.openai.com"          → append /v1/models
//   - "https://openrouter.ai/api/v1"    → append /models (don't double /v1)
//   - "https://example.com/some/prefix" → append /v1/models
//
// The earlier version blindly appended /v1/models, which broke
// OpenRouter (baseURL already includes /api/v1) and produced a 404
// → fallback to the model-name heuristic. The DeepSeek heuristic was
// 64K but real deepseek-v4-pro context is 1M, so the HUD bar showed
// 36K/64K (57%) when actual occupancy was 36K/1M (~3.6%).
func FetchModelInfo(baseURL, apiKey, modelID string) *ModelInfo {
	if baseURL == "" || apiKey == "" {
		return nil
	}

	trimmed := strings.TrimSuffix(baseURL, "/")
	var url string
	if strings.HasSuffix(trimmed, "/v1") {
		url = trimmed + "/models"
	} else {
		url = trimmed + "/v1/models"
	}
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

// modelInfoCacheTTL controls how long disk-cached /v1/models responses
// stay valid. 24h matches OpenRouter's typical model-catalog refresh
// cadence and keeps altcode startup fast (the live API call adds 1-3s
// per cold launch).
const modelInfoCacheTTL = 24 * time.Hour

var modelCacheMu sync.Mutex

// FetchModelInfoCached is the production wrapper around FetchModelInfo
// that adds a 24h disk cache. Use this from the engine startup path so
// the /v1/models call only fires once per day per (baseURL,modelID),
// instead of every altcode launch. On a cache miss it falls through
// to FetchModelInfo and writes the result back to disk.
//
// Cache file: $HOME/.altcode/models-cache.json
//   { "<sha1(baseURL|modelID)>": { "info": {...}, "fetched_at": "..." } }
//
// Race-safe (sync.Mutex) but not multi-process-safe — collisions just
// produce a slightly more aggressive refresh, never stale data.
func FetchModelInfoCached(baseURL, apiKey, modelID string) *ModelInfo {
	if baseURL == "" || apiKey == "" {
		return nil
	}
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()

	path := modelCachePath()
	key := modelCacheKey(baseURL, modelID)
	cache := loadModelCache(path)

	if entry, ok := cache[key]; ok && time.Since(entry.FetchedAt) < modelInfoCacheTTL {
		return &entry.Info
	}

	// Live fetch (releases lock briefly to avoid blocking parallel
	// providers; deferred Lock above re-acquires implicitly through
	// the for-defer dance — actually we hold the lock through the
	// network call. This is fine for typical 1-3 provider startups
	// and keeps the cache write trivially atomic).
	info := FetchModelInfo(baseURL, apiKey, modelID)
	if info == nil {
		return nil
	}
	cache[key] = modelCacheEntry{Info: *info, FetchedAt: time.Now()}
	saveModelCache(path, cache)
	return info
}

type modelCacheEntry struct {
	Info      ModelInfo `json:"info"`
	FetchedAt time.Time `json:"fetched_at"`
}

func modelCacheKey(baseURL, modelID string) string {
	h := sha1.Sum([]byte(strings.TrimSuffix(baseURL, "/") + "|" + modelID))
	return hex.EncodeToString(h[:])
}

func modelCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".altcode", "models-cache.json")
}

func loadModelCache(path string) map[string]modelCacheEntry {
	out := map[string]modelCacheEntry{}
	if path == "" {
		return out
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(body, &out)
	return out
}

func saveModelCache(path string, m map[string]modelCacheEntry) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, body, 0o600)
}
