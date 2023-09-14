package hlsboost

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/zjx20/urlproxy/urlopts"
)

var (
	headerSkipHLSBoost = http.CanonicalHeaderKey("X-Urlproxy-Skip-HLS-Boost")
)

type SelfClient struct {
	scheme string
	addr   string
}

func NewSelfClient(scheme string, addr string) *SelfClient {
	return &SelfClient{
		scheme: scheme,
		addr:   addr,
	}
}

func (h *SelfClient) Get(ctx context.Context, relativeToPath string, uri string,
	opts *urlopts.Options) (*http.Response, error) {
	url := h.ToFinalUrl(relativeToPath, uri, opts)
	selfReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	manipulateRequestToSkipHlsBoost(selfReq)
	return http.DefaultClient.Do(selfReq)
}

func (h *SelfClient) ToFinalUrl(relativeToPath string, uri string,
	opts *urlopts.Options) string {
	path := toUrlproxyURI(relativeToPath, uri, opts)
	return fmt.Sprintf("%s://%s%s", h.scheme, h.addr, path)
}

func manipulateRequestToSkipHlsBoost(req *http.Request) *http.Request {
	req.Header.Set(headerSkipHLSBoost, "true")
	return req
}

func toUrlproxyURI(relativeToPath string, uri string, opts *urlopts.Options) string {
	if uri == "" {
		return uri
	}
	baseUrl, err := url.Parse(relativeToPath)
	if err != nil {
		return uri
	}
	u, err := baseUrl.Parse(uri)
	if err != nil {
		return uri
	}
	u = urlopts.RelocateToUrlproxy(u, opts)
	return u.String()
}
