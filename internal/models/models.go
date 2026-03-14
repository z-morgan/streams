package models

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zmorgan/streams/internal/ollama"
)

// Aliases are always available, regardless of API key.
var Aliases = []string{"default", "sonnet", "opus", "haiku"}

// ModelEntry represents a discovered model from the API.
type ModelEntry struct {
	ID     string
	Family string // e.g. "claude-sonnet-4", derived from the model ID
}

// Fetcher asynchronously fetches available models from the Anthropic API and Ollama.
type Fetcher struct {
	mu            sync.RWMutex
	models        []ModelEntry
	fetched       bool
	fetchErr      error
	ollamaModels  []ollama.Model
	ollamaFetched bool
}

// FetchAsync starts a background goroutine to fetch models from the API.
// Safe to call multiple times; only the first call triggers a fetch.
func (f *Fetcher) FetchAsync() {
	f.mu.Lock()
	if f.fetched {
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()

	go func() {
		models, err := fetchModels()
		f.mu.Lock()
		f.models = models
		f.fetchErr = err
		f.fetched = true
		f.mu.Unlock()
		if err != nil {
			slog.Debug("model discovery failed", "err", err)
		} else {
			slog.Debug("model discovery complete", "count", len(models))
		}
	}()

	go func() {
		ollamaModels, err := ollama.ListModels()
		f.mu.Lock()
		if err == nil {
			f.ollamaModels = ollamaModels
		} else {
			slog.Debug("ollama model discovery failed", "err", err)
		}
		f.ollamaFetched = true
		f.mu.Unlock()
	}()
}

// Models returns the list of discovered API models.
// Returns nil if the fetch hasn't completed or failed.
func (f *Fetcher) Models() []ModelEntry {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.models
}

// AllOptions returns the full ordered list: aliases, then API models, then Ollama models.
func (f *Fetcher) AllOptions() []string {
	result := make([]string, len(Aliases))
	copy(result, Aliases)

	apiModels := f.Models()
	for _, m := range apiModels {
		result = append(result, m.ID)
	}

	for _, m := range f.OllamaOptions() {
		result = append(result, m)
	}
	return result
}

// OllamaOptions returns Ollama model names with the "ollama:" prefix.
func (f *Fetcher) OllamaOptions() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var result []string
	for _, m := range f.ollamaModels {
		result = append(result, "ollama:"+m.Name)
	}
	return result
}

// IsOllamaModel returns true if the model name has the "ollama:" prefix.
func IsOllamaModel(name string) bool {
	return strings.HasPrefix(name, "ollama:")
}

// OllamaRunning checks if the Ollama server is reachable.
func (f *Fetcher) OllamaRunning() bool {
	return ollama.IsRunning()
}

// apiResponse matches the Anthropic /v1/models response shape.
type apiResponse struct {
	Data []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"data"`
}

func fetchModels() ([]ModelEntry, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, nil // no key = no API models, not an error
	}

	req, err := http.NewRequest("GET", "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelEntry
	for _, m := range result.Data {
		if !strings.HasPrefix(m.ID, "claude-") {
			continue
		}
		models = append(models, ModelEntry{
			ID:     m.ID,
			Family: modelFamily(m.ID),
		})
	}

	sort.Slice(models, func(i, j int) bool {
		if models[i].Family != models[j].Family {
			return models[i].Family < models[j].Family
		}
		return models[i].ID < models[j].ID
	})

	return models, nil
}

// modelFamily extracts the family prefix from a model ID.
// e.g. "claude-sonnet-4-6-20250514" → "claude-sonnet-4"
func modelFamily(id string) string {
	parts := strings.Split(id, "-")
	// Keep "claude-<variant>-<major>" as the family
	if len(parts) >= 3 {
		return strings.Join(parts[:3], "-")
	}
	return id
}
