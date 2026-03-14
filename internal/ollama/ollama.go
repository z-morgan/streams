package ollama

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultBaseURL = "http://localhost:11434"

// Model represents a locally available Ollama model.
type Model struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// IsRunning checks whether the Ollama server is reachable.
func IsRunning() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(defaultBaseURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ListModels returns the locally available models from the Ollama API.
func ListModels() ([]Model, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(defaultBaseURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama API returned status %d", resp.StatusCode)
	}

	var result struct {
		Models []Model `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}

	return result.Models, nil
}
