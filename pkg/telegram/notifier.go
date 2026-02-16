// Package telegram handles sending deal notifications via Telegram Bot API.
//
// Uses direct HTTP calls to the Bot API (no heavy SDK).
// Supports sending photos with formatted captions.
package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Notifier sends deal alerts to Telegram.
type Notifier struct {
	botToken string
	chatID   string
	client   *http.Client
	apiBase  string
}

// NewNotifier creates a Telegram notifier.
func NewNotifier(botToken, chatID string) *Notifier {
	return &Notifier{
		botToken: botToken,
		chatID:   chatID,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiBase: "https://api.telegram.org/bot",
	}
}

// ---------- Telegram API request/response structs ----------

type sendPhotoRequest struct {
	ChatID    string `json:"chat_id"`
	Photo     string `json:"photo"` // URL of the image
	Caption   string `json:"caption"`
	ParseMode string `json:"parse_mode"` // "HTML" or "MarkdownV2"
}

type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

type telegramResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

type update struct {
	UpdateID int      `json:"update_id"`
	Message  *message `json:"message"`
}

type message struct {
	Chat *chat  `json:"chat"`
	Text string `json:"text"`
}

type chat struct {
	ID int64 `json:"id"`
}

// ---------- Public methods ----------

// DealItem holds the info needed to send a deal notification.
type DealItem struct {
	Name      string
	Price     int
	BrandName string
	ImageURL  string
	ItemURL   string
	AgeMin    float64
}

// SendDeal sends a formatted deal notification with product photo.
func (n *Notifier) SendDeal(deal DealItem) error {
	caption := formatDealCaption(deal)

	if deal.ImageURL != "" {
		return n.sendPhoto(deal.ImageURL, caption)
	}
	return n.sendMessage(caption)
}

// SendStartup sends a startup notification.
func (n *Notifier) SendStartup(brandCount int, scanInterval int) error {
	msg := fmt.Sprintf(
		"🤖 <b>AutoBot Started!</b>\n\n"+
			"🔍 Watching <b>%d brands</b>\n"+
			"⏰ Scan interval: <b>%d minutes</b>\n"+
			"🕐 Time: %s\n\n"+
			"🟢 Ready to hunt deals!",
		brandCount,
		scanInterval,
		time.Now().Format("2006-01-02 15:04 MST"),
	)
	return n.sendMessage(msg)
}

// SendError sends an error notification (for critical errors only).
func (n *Notifier) SendError(errMsg string) error {
	msg := fmt.Sprintf("🔴 <b>AutoBot Error</b>\n\n<code>%s</code>", escapeHTML(errMsg))
	return n.sendMessage(msg)
}

// SendScanSummary sends a summary after each scan cycle.
func (n *Notifier) SendScanSummary(totalFound, totalNew, totalKept int, duration time.Duration) error {
	if totalNew == 0 {
		return nil // don't spam if nothing new
	}
	msg := fmt.Sprintf(
		"📊 <b>Scan Complete</b>\n"+
			"Found: %d | New: %d | Sent: %d\n"+
			"⏱ %s",
		totalFound, totalNew, totalKept,
		duration.Round(time.Second),
	)
	return n.sendMessage(msg)
}

// TestConnection sends a test message to verify bot + chat ID work.
func (n *Notifier) TestConnection() error {
	msg := "🧪 <b>AutoBot Test</b>\n\nTelegram connection successful! ✅"
	return n.sendMessage(msg)
}

// ListenForCommands starts a long-polling loop to listen for /check commands.
// It matches the specific chatID to prevent unauthorized access.
func (n *Notifier) ListenForCommands(stopChan <-chan struct{}, getStatus func() string) {
	offset := 0

	for {
		select {
		case <-stopChan:
			return
		default:
			// Poll updates
			updates, newOffset, err := n.getUpdates(offset)
			if err != nil {
				// Log error but verify it's not just a timeout
				time.Sleep(5 * time.Second) // backoff
				continue
			}
			offset = newOffset

			for _, up := range updates {
				if up.Message == nil || up.Message.Text == "" {
					continue
				}

				// Security check: only allow configured chatID
				if fmt.Sprintf("%d", up.Message.Chat.ID) != n.chatID {
					continue
				}

				if strings.HasPrefix(up.Message.Text, "/check") || strings.HasPrefix(up.Message.Text, "/status") {
					statusMsg := getStatus()
					_ = n.sendMessage(statusMsg)
				}
			}

			// Small sleep to prevent tight loops if polling is fast
			time.Sleep(1 * time.Second)
		}
	}
}

func (n *Notifier) getUpdates(offset int) ([]update, int, error) {
	url := fmt.Sprintf("%s%s/getUpdates?offset=%d&timeout=10", n.apiBase, n.botToken, offset)
	resp, err := n.client.Get(url)
	if err != nil {
		return nil, offset, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, offset, fmt.Errorf("bad status: %d", resp.StatusCode)
	}

	var tgResp telegramResponse
	if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
		return nil, offset, err
	}

	if !tgResp.OK {
		return nil, offset, fmt.Errorf("api error: %s", tgResp.Description)
	}

	var updates []update
	if err := json.Unmarshal(tgResp.Result, &updates); err != nil {
		return nil, offset, err
	}

	if len(updates) > 0 {
		// Next offset is last update_id + 1
		offset = updates[len(updates)-1].UpdateID + 1
	}

	return updates, offset, nil
}

// ---------- Private methods ----------

func (n *Notifier) sendPhoto(photoURL, caption string) error {
	req := sendPhotoRequest{
		ChatID:    n.chatID,
		Photo:     photoURL,
		Caption:   caption,
		ParseMode: "HTML",
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling photo request: %w", err)
	}

	url := n.apiBase + n.botToken + "/sendPhoto"
	return n.doRequest(url, body)
}

func (n *Notifier) sendMessage(text string) error {
	req := sendMessageRequest{
		ChatID:    n.chatID,
		Text:      text,
		ParseMode: "HTML",
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling message request: %w", err)
	}

	url := n.apiBase + n.botToken + "/sendMessage"
	return n.doRequest(url, body)
}

func (n *Notifier) doRequest(url string, body []byte) error {
	resp, err := n.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading telegram response: %w", err)
	}

	var tgResp telegramResponse
	if err := json.Unmarshal(respBody, &tgResp); err != nil {
		return fmt.Errorf("parsing telegram response: %w", err)
	}

	if !tgResp.OK {
		return fmt.Errorf("telegram API error: %s", tgResp.Description)
	}

	return nil
}

// ---------- Formatting ----------

func formatDealCaption(deal DealItem) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🔥 <b>%s</b>\n", escapeHTML(deal.Name)))
	sb.WriteString(fmt.Sprintf("💰 ¥%s\n", formatPrice(deal.Price)))

	if deal.BrandName != "" {
		sb.WriteString(fmt.Sprintf("🏷 %s\n", escapeHTML(deal.BrandName)))
	}

	sb.WriteString(fmt.Sprintf("📦 Posted %.0f min ago\n", deal.AgeMin))
	sb.WriteString(fmt.Sprintf("🔗 <a href=\"%s\">View on Mercari</a>", deal.ItemURL))

	return sb.String()
}

func formatPrice(price int) string {
	// Format with thousand separator: 15000 → 15,000
	s := fmt.Sprintf("%d", price)
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
