// Package config handles loading and validating AutoBot configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the root configuration struct loaded from config.json.
type Config struct {
	Telegram  TelegramConfig `json:"telegram"`
	HuggingFace HFConfig    `json:"huggingface"`
	Brands    []Brand        `json:"brands"`

	// Search parameters
	PriceMin          int    `json:"price_min"`
	PriceMax          int    `json:"price_max"`
	ScanIntervalMin   int    `json:"scan_interval_minutes"`
	MaxAgeMinutes     int    `json:"max_age_minutes"`
	MaxDealsPerBrand  int    `json:"max_deals_per_keyword"`
	DefaultCategories []int  `json:"default_categories"`

	// AI Filter
	EnableAIFilter bool `json:"enable_ai_filter"`
}

// TelegramConfig holds Telegram Bot credentials.
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// HFConfig holds HuggingFace Inference API credentials.
type HFConfig struct {
	APIKey string `json:"api_key"` // free tier key from huggingface.co/settings/tokens
	Model  string `json:"model"`   // default: openai/clip-vit-large-patch14
}

// Brand represents a brand to search with multiple keywords.
type Brand struct {
	Name     string   `json:"name"`
	Keywords []string `json:"keywords"`
	PriceMin int      `json:"price_min,omitempty"` // override global if set
	PriceMax int      `json:"price_max,omitempty"` // override global if set
}

// LoadConfig reads and validates config from a JSON file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config JSON: %w", err)
	}

	// Apply defaults
	if cfg.ScanIntervalMin <= 0 {
		cfg.ScanIntervalMin = 10
	}
	if cfg.MaxAgeMinutes <= 0 {
		cfg.MaxAgeMinutes = 180
	}
	if cfg.MaxDealsPerBrand <= 0 {
		cfg.MaxDealsPerBrand = 5
	}
	if cfg.PriceMin <= 0 {
		cfg.PriceMin = 3000
	}
	if cfg.PriceMax <= 0 {
		cfg.PriceMax = 15000
	}
	if len(cfg.DefaultCategories) == 0 {
		cfg.DefaultCategories = []int{1, 2} // Fashion Men/Women
	}
	if cfg.HuggingFace.Model == "" {
		cfg.HuggingFace.Model = "openai/clip-vit-large-patch14"
	}

	// Validate required fields
	if cfg.Telegram.BotToken == "" {
		return nil, fmt.Errorf("telegram.bot_token is required")
	}
	if cfg.Telegram.ChatID == "" {
		return nil, fmt.Errorf("telegram.chat_id is required")
	}
	if len(cfg.Brands) == 0 {
		return nil, fmt.Errorf("at least one brand is required")
	}

	return cfg, nil
}

// GetPriceRange returns the effective price range for a brand,
// using brand-specific overrides if set, otherwise global defaults.
func (c *Config) GetPriceRange(brand Brand) (int, int) {
	pMin := c.PriceMin
	pMax := c.PriceMax
	if brand.PriceMin > 0 {
		pMin = brand.PriceMin
	}
	if brand.PriceMax > 0 {
		pMax = brand.PriceMax
	}
	return pMin, pMax
}
