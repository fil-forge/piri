package receipts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/fil-forge/ucantone/ucan"
	"github.com/fil-forge/ucantone/ucan/container"
	"github.com/ipfs/go-cid"
)

var ErrNotFound = errors.New("receipt not found")

type Client struct {
	endpoint *url.URL
	client   *http.Client
}

type Option func(c *Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.client = client
	}
}

func NewClient(endpoint *url.URL, options ...Option) *Client {
	c := Client{endpoint: endpoint}
	for _, o := range options {
		o(&c)
	}
	if c.client == nil {
		c.client = &http.Client{
			Timeout: 10 * time.Second,
		}
	}
	return &c
}

// Fetch a receipt from the receipt API. Returns [ErrNotFound] if the API
// responds with [http.StatusNotFound].
func (c *Client) Fetch(ctx context.Context, lnk cid.Cid) (ucan.Receipt, error) {
	receiptURL := c.endpoint.JoinPath(lnk.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, receiptURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating get request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doing receipts request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	ct, err := container.Decode(body)
	if err != nil {
		return nil, fmt.Errorf("decoding container: %w", err)
	}

	for _, rcpt := range ct.Receipts() {
		if rcpt.Link().Equals(lnk) {
			return rcpt, nil
		}
	}
	return nil, fmt.Errorf("receipt %s not found in container", lnk)
}
