package client

import (
	"fmt"
	"net/http"
)

type customHeadersRoundTripper struct {
	header                http.Header
	delegatedRoundTripper http.RoundTripper
}

func NewCustomHeadersRoundTripper(header http.Header, rt http.RoundTripper) http.RoundTripper {
	return &customHeadersRoundTripper{
		header:                header,
		delegatedRoundTripper: rt,
	}
}

var _ http.RoundTripper = &customHeadersRoundTripper{}

func (c *customHeadersRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, values := range c.header {
		if req.Header.Get(key) != "" {
			return nil, fmt.Errorf("custom header %q can't override other header", key)
		}
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	return c.delegatedRoundTripper.RoundTrip(req)
}
