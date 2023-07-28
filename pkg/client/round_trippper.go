package client

import "net/http"

type customHeadersRoundTripper struct {
	delegatedRoundTripper http.RoundTripper
	headers               http.Header
}

func NewCustomHeadersRoundTripper(headers http.Header, rt http.RoundTripper) http.RoundTripper {
	return &customHeadersRoundTripper{
		delegatedRoundTripper: rt,
		headers:               headers,
	}
}

var _ http.RoundTripper = &customHeadersRoundTripper{}

func (c *customHeadersRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, values := range c.headers {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	return c.delegatedRoundTripper.RoundTrip(req)
}
