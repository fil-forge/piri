package receipts

/*

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/fil-forge/go-ucanto/transport"
	"github.com/fil-forge/go-ucanto/transport/car"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/ipfs/go-cid"
)

var ErrNotFound = errors.New("receipt not found")

type Client struct {
	endpoint *url.URL
	client   *http.Client
	codec    transport.ResponseDecoder
}

type Option func(c *Client)

func NewClient(endpoint *url.URL, options ...Option) *Client {
	c := Client{
		endpoint: endpoint,
		codec:    car.NewOutboundCodec(),
	}
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
func (c *Client) Fetch(ctx context.Context, lnk cid.Cid) (*receipt.Receipt, error) {
	// TODO(forrest)[ucan1]: I'd expect the etracker to work with ucan, not plain http fetch...
	// not that this fetch even matters, the only caller of this method fetches receipt
	// then does approximately nothing with them, passing them to validateConsolidateReceipt
	// which previously was stubbed, now it panics as a stubbed validation method
	// smells like a trash fire.
	panic("FIX ME FORREST - MIGRATE THE ETRACKER")
}


*/
