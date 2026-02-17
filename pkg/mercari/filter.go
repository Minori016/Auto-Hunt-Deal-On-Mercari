// Package mercari implements AI-based image filtering using HuggingFace CLIP.
//
// CLIP (Contrastive Language-Image Pre-Training) performs zero-shot image
// classification by comparing an image against text labels.
// We use it to identify "trash" items: empty boxes, shopping bags, blurry photos.
package mercari

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// AIFilter uses HuggingFace CLIP to classify item images.
type AIFilter struct {
	apiKey  string
	model   string
	client  *http.Client
	enabled bool

	// Labels for zero-shot classification
	keepLabels  []string // labels indicating a real product
	trashLabels []string // labels indicating trash
}

// NewAIFilter creates a filter. If apiKey is empty, filtering is disabled (passthrough).
func NewAIFilter(apiKey, model string, enabled bool) *AIFilter {
	return &AIFilter{
		apiKey:  apiKey,
		model:   model,
		enabled: enabled && apiKey != "" && apiKey != "YOUR_HF_API_KEY",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		keepLabels: []string{
			"a hat or cap",
			"a beanie",
			"a jacket or coat",
			"a leather jacket",
			"a sweater or knitwear",
			"a shirt or top",
			"pants or trousers",
			"shorts",
			"a designer handbag",
			"a leather bag",
			"a luxury wallet",
			"designer shoes",
			"leather shoes or boots",
			"sunglasses",
			"a watch",
			"jewelry",
			"fashion accessories",
		},
		trashLabels: []string{
			"an empty box",
			"a cardboard box",
			"a shopping bag",
			"a paper bag",
			"a receipt",
			"a blurry photo",
			"a logo tag only",
			"a dust bag only",
		},
	}
}

// clipRequest is the HuggingFace Inference API request body for CLIP.
type clipRequest struct {
	Inputs clipInputs `json:"inputs"`
}

type clipInputs struct {
	Image           string   `json:"image"` // URL of the image
	CandidateLabels []string `json:"candidate_labels"`
}

// clipResponse is the HuggingFace response.
type clipResponse struct {
	Labels []string  `json:"labels"`
	Scores []float64 `json:"scores"`
}

// FilterItems runs AI classification on items and removes trash.
// It processes images concurrently with a limited goroutine pool (RPi-safe).
func (f *AIFilter) FilterItems(items []Item) []Item {
	if !f.enabled {
		log.Println("[FILTER] AI filter disabled, passing all items through")
		return items
	}

	if len(items) == 0 {
		return items
	}

	log.Printf("[FILTER] Analyzing %d items with CLIP (%s)", len(items), f.model)

	// Process with limited concurrency (3 goroutines for RPi)
	const maxWorkers = 3
	type result struct {
		index int
		keep  bool
		label string
		score float64
	}

	results := make([]result, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)

	for i, item := range items {
		if len(item.ImageURLs) == 0 {
			results[i] = result{index: i, keep: true, label: "no_image", score: 0}
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // acquire slot

		go func(idx int, it Item) {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			keep, label, score := f.classifyItem(it)
			results[idx] = result{index: idx, keep: keep, label: label, score: score}
		}(i, item)
	}

	wg.Wait()

	// Collect kept items
	kept := make([]Item, 0)
	for i, r := range results {
		if r.keep {
			kept = append(kept, items[i])
			log.Printf("[FILTER] ✅ KEEP: '%s' (label='%s' score=%.2f)", items[i].Name, r.label, r.score)
		} else {
			log.Printf("[FILTER] ❌ TRASH: '%s' (label='%s' score=%.2f)", items[i].Name, r.label, r.score)
		}
	}

	log.Printf("[FILTER] Result: %d/%d items kept", len(kept), len(items))
	return kept
}

// classifyItem checks a single item's first image using CLIP.
// Returns (keep, topLabel, topScore).
func (f *AIFilter) classifyItem(item Item) (bool, string, float64) {
	if len(item.ImageURLs) == 0 {
		return true, "no_image", 0
	}

	imageURL := item.ImageURLs[0]

	// Combine keep + trash labels for classification
	allLabels := append(f.keepLabels, f.trashLabels...)

	reqBody := clipRequest{
		Inputs: clipInputs{
			Image:           imageURL,
			CandidateLabels: allLabels,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("[FILTER] Error marshaling request: %v", err)
		return true, "error", 0 // fail-open: keep item on error
	}

	apiURL := fmt.Sprintf("https://router.huggingface.co/hf-inference/models/%s", f.model)
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		log.Printf("[FILTER] Error creating request: %v", err)
		return true, "error", 0
	}

	req.Header.Set("Authorization", "Bearer "+f.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		log.Printf("[FILTER] API request failed: %v", err)
		return true, "error", 0 // fail-open
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[FILTER] Error reading response: %v", err)
		return true, "error", 0
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[FILTER] HuggingFace API returned %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
		// If model is loading, wait and retry once
		if resp.StatusCode == 503 {
			log.Println("[FILTER] Model is loading, waiting 20s and retrying...")
			time.Sleep(20 * time.Second)
			return f.classifyItem(item) // retry once
		}
		return true, "api_error", 0 // fail-open
	}

	// Parse response — can be a single object or an array
	var clipResp clipResponse
	if err := json.Unmarshal(body, &clipResp); err != nil {
		// Try as array (some models return [{ labels: ..., scores: ... }])
		var arr []clipResponse
		if err2 := json.Unmarshal(body, &arr); err2 != nil || len(arr) == 0 {
			log.Printf("[FILTER] Error parsing response: %v / %v (body: %s)", err, err2, string(body[:min(len(body), 200)]))
			return true, "parse_error", 0
		}
		clipResp = arr[0]
	}

	if len(clipResp.Labels) == 0 || len(clipResp.Scores) == 0 {
		return true, "empty_result", 0
	}

	// Top label is the first one (highest score)
	topLabel := clipResp.Labels[0]
	topScore := clipResp.Scores[0]

	// Check if top label is a trash label
	for _, trashLabel := range f.trashLabels {
		if topLabel == trashLabel && topScore > 0.3 {
			return false, topLabel, topScore // TRASH
		}
	}

	return true, topLabel, topScore // KEEP
}
