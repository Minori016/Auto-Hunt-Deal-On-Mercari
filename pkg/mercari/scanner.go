// Package mercari implements the Mercari Japan search API client.
//
// Uses Mercari's internal v2 search endpoint with DPoP JWT authentication.
// The DPoP token is generated using ECDSA P-256, matching the web app's mechanism.
// Optimized for low memory usage on Raspberry Pi.
package mercari

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	searchAPIURL = "https://api.mercari.jp/v2/entities:search"
)

// Scanner searches Mercari Japan for items using the internal API.
type Scanner struct {
	client     *http.Client
	privateKey *ecdsa.PrivateKey
	userAgent  string
}

// NewScanner creates a new Mercari scanner with DPoP key pair.
func NewScanner() *Scanner {
	// Generate fresh ECDSA P-256 key pair for DPoP tokens
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("[SCANNER] Failed to generate ECDSA key: %v", err)
	}

	return &Scanner{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        5,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     60 * time.Second,
				DisableCompression:  false,
			},
		},
		privateKey: privateKey,
		userAgent:  randomUserAgent(),
	}
}

// ---------- DPoP JWT Token Generation ----------

// dpopHeader is the JWT header for DPoP tokens.
type dpopHeader struct {
	Typ string     `json:"typ"`
	Alg string     `json:"alg"`
	JWK dpopJWK    `json:"jwk"`
}

// dpopJWK contains the public key in JWK format.
type dpopJWK struct {
	Crv string `json:"crv"`
	Kty string `json:"kty"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// dpopPayload is the JWT payload for DPoP.
type dpopPayload struct {
	IAT  int64  `json:"iat"`
	JTI  string `json:"jti"`
	HTU  string `json:"htu"`
	HTM  string `json:"htm"`
	UUID string `json:"uuid"`
}

// generateDPoP creates a DPoP JWT token for the given URL and method.
func (s *Scanner) generateDPoP(apiURL, method string) (string, error) {
	pubKey := &s.privateKey.PublicKey

	// Encode public key coordinates as base64url (unpadded)
	xBytes := pubKey.X.Bytes()
	yBytes := pubKey.Y.Bytes()
	// Pad to 32 bytes (P-256 key size)
	xPadded := padTo32(xBytes)
	yPadded := padTo32(yBytes)

	header := dpopHeader{
		Typ: "dpop+jwt",
		Alg: "ES256",
		JWK: dpopJWK{
			Crv: "P-256",
			Kty: "EC",
			X:   base64URLEncode(xPadded),
			Y:   base64URLEncode(yPadded),
		},
	}

	payload := dpopPayload{
		IAT:  time.Now().Unix(),
		JTI:  generateUUID(),
		HTU:  apiURL,
		HTM:  method,
		UUID: generateUUID(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	// Create signing input: base64url(header).base64url(payload)
	signingInput := base64URLEncode(headerJSON) + "." + base64URLEncode(payloadJSON)

	// Sign with ECDSA P-256 SHA-256
	hash := sha256.Sum256([]byte(signingInput))
	r, ss, err := ecdsa.Sign(rand.Reader, s.privateKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("ecdsa sign: %w", err)
	}

	// Encode signature as r||s (each 32 bytes)
	rBytes := padTo32(r.Bytes())
	sBytes := padTo32(ss.Bytes())
	signature := append(rBytes, sBytes...)

	// Final JWT: header.payload.signature
	jwt := signingInput + "." + base64URLEncode(signature)
	return jwt, nil
}

// ---------- Search API ----------

// searchRequest is the body for Mercari's v2 search API.
type searchRequest struct {
	PageSize           int             `json:"pageSize"`
	SearchSessionID    string          `json:"searchSessionId"`
	SearchCondition    searchCondition `json:"searchCondition"`
	ServiceFrom        string          `json:"serviceFrom"`
	WithItemBrand      bool            `json:"withItemBrand"`
	WithItemSize       bool            `json:"withItemSize"`
	WithItemPromotions bool            `json:"withItemPromotions"`
	WithItemSizes      bool            `json:"withItemSizes"`
	WithShopname       bool            `json:"withShopname"`
}

type searchCondition struct {
	Keyword         string   `json:"keyword"`
	ExcludeKeyword  string   `json:"excludeKeyword"`
	Sort            string   `json:"sort"`
	Order           string   `json:"order"`
	Status          []string `json:"status"`
	SizeID          []int    `json:"sizeId"`
	CategoryID      []int    `json:"categoryId"`
	BrandID         []int    `json:"brandId"`
	SellerID        []string `json:"sellerId"`
	PriceMin        int      `json:"priceMin"`
	PriceMax        int      `json:"priceMax"`
	ItemConditionID []int    `json:"itemConditionId"`
	ShippingPayerID []int    `json:"shippingPayerId"`
	ColorID         []int    `json:"colorId"`
	HasCoupon       bool     `json:"hasCoupon"`
	Attributes      []string `json:"attributes"`
	ItemTypes       []string `json:"itemTypes"`
	SkuIDs          []string `json:"skuIds"`
}

// searchResponse is the API response.
// Mercari returns some numeric fields as strings, so we use json.Number.
type searchAPIResponse struct {
	Items []searchAPIItem `json:"items"`
	Meta  struct {
		NumFound      json.Number `json:"numFound"`
		NextPageToken string      `json:"nextPageToken"`
		HasNext       bool        `json:"hasNext"`
	} `json:"meta"`
}

type searchAPIItem struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Price           json.Number `json:"price"`
	Status          string      `json:"status"`
	Created         json.Number `json:"created"`
	Updated         json.Number `json:"updated"`
	Thumbnails      []string    `json:"thumbnails"`
	ItemType        string      `json:"itemType"`
	BuyerID         string      `json:"buyerId"`
	SellerID        string      `json:"sellerId"`
	ItemBrand       *struct {
		ID   json.Number `json:"id"`
		Name string      `json:"name"`
	} `json:"itemBrand"`
	ItemConditionID json.Number `json:"itemConditionId"`
}

// Search queries Mercari for items matching the given criteria.
func (s *Scanner) Search(keyword string, priceMin, priceMax int, categories []int, limit int) ([]Item, error) {
	// Generate DPoP token for this request
	dpopToken, err := s.generateDPoP(searchAPIURL, "POST")
	if err != nil {
		return nil, fmt.Errorf("generating DPoP token: %w", err)
	}

	// Build request body
	reqBody := searchRequest{
		PageSize:        limit,
		SearchSessionID: generateUUID(),
		SearchCondition: searchCondition{
			Keyword:    keyword,
			Sort:       "SORT_CREATED_TIME",
			Order:      "ORDER_DESC",
			Status:     []string{"STATUS_ON_SALE"},
			CategoryID: categories,
			PriceMin:   priceMin,
			PriceMax:   priceMax,
		},
		ServiceFrom:        "suruga",
		WithItemBrand:      true,
		WithItemSize:       false,
		WithItemPromotions: true,
		WithItemSizes:      true,
		WithShopname:       false,
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", searchAPIURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers — DPoP is the critical auth header
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("DPoP", dpopToken)
	req.Header.Set("X-Platform", "web")
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept-Language", "ja-JP,ja;q=0.9,en;q=0.8")
	req.Header.Set("Origin", "https://jp.mercari.com")
	req.Header.Set("Referer", "https://jp.mercari.com/")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mercari API returned %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	// Parse response
	var apiResp searchAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	// Convert to our Item type
	items := make([]Item, 0, len(apiResp.Items))
	for _, raw := range apiResp.Items {
		brandName := ""
		if raw.ItemBrand != nil {
			brandName = raw.ItemBrand.Name
		}

		price := jsonNumberToInt(raw.Price)
		createdTS := jsonNumberToInt64(raw.Created)
		updatedTS := jsonNumberToInt64(raw.Updated)

		created := time.Unix(createdTS, 0)
		updated := time.Unix(updatedTS, 0)

		items = append(items, Item{
			ID:        raw.ID,
			Name:      raw.Name,
			Price:     price,
			Status:    raw.Status,
			ImageURLs: raw.Thumbnails,
			Created:   created,
			Updated:   updated,
			BrandName: brandName,
			ItemURL:   "https://jp.mercari.com/item/" + raw.ID,
		})
	}

	numFound, _ := apiResp.Meta.NumFound.Int64()
	log.Printf("[SCANNER] '%s': API returned %d items (total: %d)",
		keyword, len(items), numFound)

	return items, nil
}

// SearchWithFallback tries the API. On failure, logs and returns error.
func (s *Scanner) SearchWithFallback(keyword string, priceMin, priceMax int, categories []int, limit int) ([]Item, error) {
	items, err := s.Search(keyword, priceMin, priceMax, categories, limit)
	if err != nil {
		return nil, fmt.Errorf("search failed for '%s': %w", keyword, err)
	}
	return items, nil
}

// ---------- Utility Functions ----------

// base64URLEncode encodes bytes to base64url (no padding).
func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// padTo32 pads a byte slice to exactly 32 bytes (for P-256 coordinates).
func padTo32(b []byte) []byte {
	if len(b) >= 32 {
		return b[:32]
	}
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

// generateUUID creates a simple UUID v4.
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// truncate limits a string to maxLen characters.
func truncate(s string, maxLen int) string {
	// Clean non-printable characters
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			return -1
		}
		return r
	}, s)
	if len(cleaned) > maxLen {
		return cleaned[:maxLen] + "..."
	}
	return cleaned
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
}

func randomUserAgent() string {
	return userAgents[mrand.Intn(len(userAgents))]
}

// jsonNumberToInt converts a json.Number to int, returning 0 on error.
func jsonNumberToInt(n json.Number) int {
	if n == "" {
		return 0
	}
	v, err := n.Int64()
	if err != nil {
		// Try parsing as float first (some APIs return "12345.0")
		f, err2 := n.Float64()
		if err2 != nil {
			// Try raw string parse
			i, _ := strconv.Atoi(string(n))
			return i
		}
		return int(f)
	}
	return int(v)
}

// jsonNumberToInt64 converts a json.Number to int64, returning 0 on error.
func jsonNumberToInt64(n json.Number) int64 {
	if n == "" {
		return 0
	}
	v, err := n.Int64()
	if err != nil {
		f, err2 := n.Float64()
		if err2 != nil {
			i, _ := strconv.ParseInt(string(n), 10, 64)
			return i
		}
		return int64(f)
	}
	return v
}
