// AutoBot — Mercari Japan Deal Hunter
//
// Inspired by PicoClaw's ultra-lightweight architecture.
// Scans Mercari JP for designer brand deals, filters trash via AI (CLIP),
// and sends alerts to Telegram. Designed for 24/7 Raspberry Pi operation.
//
// Usage:
//
//	go run ./cmd/autobot/                     # normal mode (loop)
//	go run ./cmd/autobot/ --once              # single scan cycle
//	go run ./cmd/autobot/ --test-telegram     # test Telegram connection
//	go run ./cmd/autobot/ --config path.json  # custom config path
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/xuhoa/autobot/config"
	"github.com/xuhoa/autobot/pkg/mercari"
	"github.com/xuhoa/autobot/pkg/store"
	"github.com/xuhoa/autobot/pkg/telegram"
)

const (
	version = "1.0.0"
	logo    = "🤖"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.json", "Path to config.json")
	once := flag.Bool("once", false, "Run one scan cycle and exit")
	testTg := flag.Bool("test-telegram", false, "Send a test Telegram message and exit")
	flag.Parse()

	// Banner
	fmt.Printf("\n%s AutoBot v%s — Mercari Deal Hunter\n", logo, version)
	fmt.Printf("   Platform: %s/%s | PID: %d\n\n", runtime.GOOS, runtime.GOARCH, os.Getpid())

	// Load config
	cfgPath := resolveConfigPath(*configPath)
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("❌ Config error: %v", err)
	}
	log.Printf("✅ Config loaded: %d brands, scan every %d min, price ¥%d-¥%d",
		len(cfg.Brands), cfg.ScanIntervalMin, cfg.PriceMin, cfg.PriceMax)

	// Init components
	scanner := mercari.NewScanner()
	filter := mercari.NewAIFilter(cfg.HuggingFace.APIKey, cfg.HuggingFace.Model, cfg.EnableAIFilter)
	notifier := telegram.NewNotifier(cfg.Telegram.BotToken, cfg.Telegram.ChatID)

	// Init dedup store (SQLite)
	dbPath := filepath.Join(filepath.Dir(cfgPath), "autobot_seen.db")
	dedupStore, err := store.NewDedupStore(dbPath)
	if err != nil {
		log.Fatalf("❌ Database error: %v", err)
	}
	defer dedupStore.Close()
	log.Printf("✅ Dedup store: %s (%d items tracked)", dbPath, dedupStore.Count())

	// Test Telegram mode
	if *testTg {
		log.Println("📤 Sending test message to Telegram...")
		if err := notifier.TestConnection(); err != nil {
			log.Fatalf("❌ Telegram test failed: %v", err)
		}
		log.Println("✅ Telegram test successful!")
		return
	}

	// Create the bot
	bot := &Bot{
		cfg:      cfg,
		scanner:  scanner,
		filter:   filter,
		notifier: notifier,
		store:    dedupStore,
	}

	if *once {
		// Single scan
		log.Println("🔍 Running single scan cycle...")
		bot.runScanCycle()
		return
	}

	// Main loop with panic recovery
	bot.startTime = time.Now()
	bot.run()
}

// Bot holds all components and runs the main scan loop.
type Bot struct {
	cfg      *config.Config
	scanner  *mercari.Scanner
	filter   *mercari.AIFilter
	notifier *telegram.Notifier
	store    *store.DedupStore

	// Status tracking
	startTime    time.Time
	lastScanTime time.Time
	runCount     int
}

// run starts the main bot loop with graceful shutdown.
func (b *Bot) run() {
	// Send startup notification
	if err := b.notifier.SendStartup(len(b.cfg.Brands), b.cfg.ScanIntervalMin); err != nil {
		log.Printf("⚠️ Failed to send startup notification: %v", err)
	}

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start Telegram command listener (for /check)
	// Create a separate stop channel for the listener since it runs in a goroutine
	listenerStop := make(chan struct{})
	go b.notifier.ListenForCommands(listenerStop, b.getStatus)
	defer close(listenerStop)

	ticker := time.NewTicker(time.Duration(b.cfg.ScanIntervalMin) * time.Minute)
	defer ticker.Stop()

	// Run first scan immediately
	log.Println("🚀 Starting first scan...")
	b.safeScan()

	log.Printf("⏰ Next scan in %d minutes. Press Ctrl+C to stop.", b.cfg.ScanIntervalMin)

	for {
		select {
		case <-ticker.C:
			b.safeScan()
			log.Printf("⏰ Next scan in %d minutes.", b.cfg.ScanIntervalMin)
		case sig := <-quit:
			log.Printf("\n🛑 Received %s, shutting down gracefully...", sig)
			return
		}
	}
}

// safeScan wraps runScanCycle with panic recovery.
func (b *Bot) safeScan() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔴 PANIC RECOVERED: %v", r)
			// Try to notify via Telegram
			_ = b.notifier.SendError(fmt.Sprintf("Panic: %v", r))
			// Wait before next cycle to avoid crash-loop
			time.Sleep(30 * time.Second)
		}
	}()
	b.runScanCycle()
}

// runScanCycle performs one complete scan of all brands.
func (b *Bot) runScanCycle() {
	start := time.Now()
	totalFound := 0
	totalNew := 0
	totalSent := 0

	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Printf("🔍 SCAN CYCLE START — %s", start.Format("15:04:05"))

	for _, brand := range b.cfg.Brands {
		found, newItems, sent := b.scanBrand(brand)
		totalFound += found
		totalNew += newItems
		totalSent += sent

		// Small delay between brands to be polite
		jitter := time.Duration(500+rand.Intn(1500)) * time.Millisecond
		time.Sleep(jitter)
	}

	duration := time.Since(start)
	log.Printf("📊 SCAN COMPLETE: found=%d new=%d sent=%d (%.1fs)",
		totalFound, totalNew, totalSent, duration.Seconds())
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if totalNew > 0 {
		_ = b.notifier.SendScanSummary(totalFound, totalNew, totalSent, duration)
	}

	b.lastScanTime = time.Now()
	b.runCount++
}

func (b *Bot) getStatus() string {
	uptime := time.Since(b.startTime).Round(time.Second)
	lastScan := "Never"
	if !b.lastScanTime.IsZero() {
		lastScan = time.Since(b.lastScanTime).Round(time.Second).String() + " ago"
	}

	return fmt.Sprintf(
		"🤖 <b>AutoBot Status</b>\n\n"+
			"✅ <b>Running</b>\n"+
			"⏳ Uptime: %s\n"+
			"🔄 Cycles: %d\n"+
			"🕒 Last scan: %s\n"+
			"📦 Items tracked: %d",
		uptime,
		b.runCount,
		lastScan,
		b.store.Count(),
	)
}

// scanBrand searches for a single brand across all its keywords.
func (b *Bot) scanBrand(brand config.Brand) (found, newItems, sent int) {
	pMin, pMax := b.cfg.GetPriceRange(brand)

	for _, keyword := range brand.Keywords {
		items, err := b.searchWithRetry(keyword, pMin, pMax, 3)
		if err != nil {
			log.Printf("[%s] ❌ Search failed for '%s': %v", brand.Name, keyword, err)
			continue
		}

		found += len(items)

		// Filter by age
		var fresh []mercari.Item
		for _, item := range items {
			age := item.AgeMinutes()
			if age <= float64(b.cfg.MaxAgeMinutes) {
				fresh = append(fresh, item)
			}
		}

		// Dedup
		var unseen []mercari.Item
		for _, item := range fresh {
			if !b.store.HasSeen(item.ID) {
				unseen = append(unseen, item)
			}
		}
		newItems += len(unseen)

		if len(unseen) == 0 {
			log.Printf("[%s] '%s': %d found, 0 new", brand.Name, keyword, len(items))
			continue
		}

		// Limit deals per keyword
		if len(unseen) > b.cfg.MaxDealsPerBrand {
			unseen = unseen[:b.cfg.MaxDealsPerBrand]
		}

		// AI Filter
		kept := b.filter.FilterItems(unseen)

		log.Printf("[%s] '%s': %d found → %d fresh → %d new → %d kept",
			brand.Name, keyword, len(items), len(fresh), len(unseen), len(kept))

		// Send notifications
		for _, item := range kept {
			deal := telegram.DealItem{
				Name:      item.Name,
				Price:     item.Price,
				BrandName: brand.Name,
				ImageURL:  firstImage(item.ImageURLs),
				ItemURL:   item.ItemURL,
				AgeMin:    item.AgeMinutes(),
			}

			if err := b.notifier.SendDeal(deal); err != nil {
				log.Printf("[%s] ⚠️ Failed to send deal: %v", brand.Name, err)
				continue
			}

			// Mark as seen (even if send fails, to avoid spam)
			_ = b.store.MarkSeen(item.ID, brand.Name, item.Name, item.Price)
			sent++

			// Rate limit: Telegram allows max 30 msg/sec, be conservative
			time.Sleep(200 * time.Millisecond)
		}
	}

	return
}

// searchWithRetry performs the search with exponential backoff on failure.
func (b *Bot) searchWithRetry(keyword string, priceMin, priceMax, maxRetries int) ([]mercari.Item, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
			log.Printf("[RETRY] Attempt %d/%d for '%s' after %v", attempt, maxRetries, keyword, backoff+jitter)
			time.Sleep(backoff + jitter)
		}

		items, err := b.scanner.SearchWithFallback(
			keyword, priceMin, priceMax,
			b.cfg.DefaultCategories,
			b.cfg.MaxDealsPerBrand*2, // fetch more than needed, filter later
		)
		if err == nil {
			return items, nil
		}

		lastErr = err
		log.Printf("[RETRY] '%s' attempt %d failed: %v", keyword, attempt, err)
	}

	return nil, fmt.Errorf("all %d retries failed: %w", maxRetries, lastErr)
}

// ---------- Helpers ----------

func resolveConfigPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	// Try relative to executable first
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		candidate := filepath.Join(exeDir, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Try relative to working directory
	if _, err := os.Stat(path); err == nil {
		abs, _ := filepath.Abs(path)
		return abs
	}

	// Try common locations
	candidates := []string{
		filepath.Join(".", path),
		filepath.Join("..", path),
	}

	// Check HOME directory
	home, _ := os.UserHomeDir()
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".autobot", path))
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}

	return path // fallback, will error on LoadConfig
}

func firstImage(urls []string) string {
	if len(urls) > 0 {
		return urls[0]
	}
	return ""
}
