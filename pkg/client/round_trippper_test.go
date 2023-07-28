package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_CustomHeadersRoundTripper(t *testing.T) {
	t.Parallel()
	t.Run("add new header", func(t *testing.T) {
		t.Parallel()
		f := &fakeRoundTripper{}
		header := make(http.Header)
		header.Set("new-header", "new-header-value")
		rt := NewCustomHeadersRoundTripper(header, f)
		req := httptest.NewRequest(http.MethodGet, "/host", nil)
		_, err := rt.RoundTrip(req)
		assert.Nil(t, err)
		assert.Equal(t, f.req.Header.Get("new-header"), "new-header-value")
	})

	t.Run("don't override exist header", func(t *testing.T) {
		t.Parallel()
		f := &fakeRoundTripper{}
		header := make(http.Header)
		header.Set("exist-header", "new-value")
		rt := NewCustomHeadersRoundTripper(header, f)
		req := httptest.NewRequest(http.MethodGet, "/host", nil)
		req.Header.Set("exist-header", "old-value")
		_, err := rt.RoundTrip(req)
		assert.NotNil(t, err)
	})
}

type fakeRoundTripper struct {
	req *http.Request
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.req = req
	return nil, nil
}
