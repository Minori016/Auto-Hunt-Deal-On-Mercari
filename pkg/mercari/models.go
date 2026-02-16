// Package mercari handles data models for Mercari Japan items.
package mercari

import "time"

// Item represents a single product listing on Mercari Japan.
type Item struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Price       int       `json:"price"`        // JPY
	Status      string    `json:"status"`        // on_sale, sold_out, etc.
	Description string    `json:"description"`
	ImageURLs   []string  `json:"image_urls"`
	Seller      string    `json:"seller_name"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
	CategoryID  int       `json:"category_id"`
	BrandName   string    `json:"brand_name"`   // matched brand from our config
	ItemURL     string    `json:"item_url"`      // full URL to item page
}

// AgeMinutes returns how many minutes ago this item was listed.
func (item *Item) AgeMinutes() float64 {
	return time.Since(item.Created).Minutes()
}

// ---------- Mercari API Response Structs ----------

// SearchResponse is the top-level response from Mercari's search API.
type SearchResponse struct {
	Items      []RawItem `json:"items"`
	Meta       MetaInfo  `json:"meta"`
}

// RawItem maps the JSON structure returned by Mercari search.
type RawItem struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Price       int           `json:"price"`
	Status      string        `json:"status"`
	Thumbnails  []string      `json:"thumbnails"`
	ImageURLs   []string      `json:"item_image_urls"`
	Created     int64         `json:"created"`
	Updated     int64         `json:"updated"`
	SellerID    string        `json:"seller_id"`
	SellerName  string        `json:"seller_name,omitempty"`
	Description string        `json:"description,omitempty"`
	CategoryID  int           `json:"category_id"`
	BrandName   string        `json:"brand_name,omitempty"`
	ItemCondID  int           `json:"item_condition_id"`
}

// MetaInfo contains pagination info.
type MetaInfo struct {
	NumFound    int    `json:"num_found"`
	NextPageToken string `json:"next_page_token,omitempty"`
	HasNext     bool   `json:"has_next"`
}

// ToItem converts a RawItem from the API into our clean Item struct.
func (r *RawItem) ToItem() Item {
	images := r.ImageURLs
	if len(images) == 0 {
		images = r.Thumbnails
	}

	created := time.Unix(r.Created, 0)
	updated := time.Unix(r.Updated, 0)

	return Item{
		ID:          r.ID,
		Name:        r.Name,
		Price:       r.Price,
		Status:      r.Status,
		Description: r.Description,
		ImageURLs:   images,
		Seller:      r.SellerName,
		Created:     created,
		Updated:     updated,
		CategoryID:  r.CategoryID,
		BrandName:   r.BrandName,
		ItemURL:     "https://jp.mercari.com/item/" + r.ID,
	}
}
