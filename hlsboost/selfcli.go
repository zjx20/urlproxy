package hlsboost

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/zjx20/urlproxy/urlopts"
)

var cli *http.Client

func init() {
	tmp := *http.DefaultClient
	cli = &tmp
	// no redirect
	cli.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
}

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
	return cli.Do(selfReq)
}

func (h *SelfClient) ToFinalUrl(relativeToPath string, uri string,
	opts *urlopts.Options) string {
	tmp := opts.Clone()
	tmp.Set(urlopts.OptHLSSkip.New(true))
	path := toUrlproxyURI(relativeToPath, uri, tmp)
	return fmt.Sprintf("%s://%s%s", h.scheme, h.addr, path)
}

func toUrlproxyURI(relativeToPath string, uri string, opts *urlopts.Options) string {
	if uri == "" {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	// if `uri` is a relative url, we should make it relative to `relativeToPath`
	if u.Scheme == "" && !strings.HasPrefix(u.Path, "/") {
		pos := strings.LastIndexByte(relativeToPath, '/')
		if pos != -1 {
			u.Path = relativeToPath[:pos+1] + u.Path
		}
	}
	u = urlopts.RelocateToUrlproxy(u, opts)
	return u.String()
}
