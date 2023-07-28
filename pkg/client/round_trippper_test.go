package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_CustomHeadersRoundTripper(t *testing.T) {
	t.Parallel()
	f := &fakeRoundTripper{}
	header := make(http.Header)
	header.Set("new-header", "new-header-value")
	rt := NewCustomHeadersRoundTripper(header, f)
	req := httptest.NewRequest(http.MethodGet, "/host", nil)
	_, err := rt.RoundTrip(req)
	assert.Nil(t, err)
	assert.Equal(t, f.req.Header.Get("new-header"), "new-header-value")
}

type fakeRoundTripper struct {
	req *http.Request
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.req = req
	return nil, nil
}
