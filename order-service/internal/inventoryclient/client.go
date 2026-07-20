package inventoryclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("item not found")
	ErrInsufficientStock = errors.New("insufficient stock")
)

type Item struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) Reserve(ctx context.Context, sku string, quantity int) (Item, error) {
	body, err := json.Marshal(map[string]int{"quantity": quantity})
	if err != nil {
		return Item{}, fmt.Errorf("encode request: %w", err)
	}

	endpoint := c.baseURL + "/items/" + url.PathEscape(sku) + "/reserve"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Item{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Item{}, fmt.Errorf("call inventory-service: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var item Item
		if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
			return Item{}, fmt.Errorf("decode response: %w", err)
		}
		return item, nil
	case http.StatusNotFound:
		return Item{}, ErrNotFound
	case http.StatusConflict:
		return Item{}, ErrInsufficientStock
	default:
		return Item{}, fmt.Errorf("inventory-service returned status %d", resp.StatusCode)
	}
}
