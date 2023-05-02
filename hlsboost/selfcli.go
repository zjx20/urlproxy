package hlsboost

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/zjx20/urlproxy/urlopts"
)

var (
	trueVal = true
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
	return http.DefaultClient.Do(selfReq)
}

func (h *SelfClient) ToFinalUrl(relativeToPath string, uri string,
	opts *urlopts.Options) string {
	tmp := *opts
	tmp.HLSSkip = &trueVal
	path := toUrlproxyURI(relativeToPath, uri, opts)
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
	cloneOpts := *opts

	if u.Scheme != "" {
		// it's an absolute url, convert it into a relative url for urlproxy
		scheme := u.Scheme
		host := u.Host
		cloneOpts.Scheme = &scheme
		cloneOpts.Host = &host
		optPath := strings.Join(urlopts.ToList(&cloneOpts), "/")
		u.Path = "/" + optPath + u.Path
		u.Scheme = ""
		u.Host = ""
	} else {
		// it's a relative url
		if !strings.HasPrefix(u.Path, "/") {
			pos := strings.LastIndexByte(relativeToPath, '/')
			if pos != -1 {
				u.Path = relativeToPath[:pos+1] + u.Path
			}
		}
		optPath := strings.Join(urlopts.ToList(&cloneOpts), "/")
		if optPath != "" {
			if strings.HasPrefix(u.Path, "/") {
				// absolute path
				u.Path = "/" + optPath + u.Path
			} else {
				// relative path
				u.Path = optPath + "/" + u.Path
			}
		}
	}
	return u.String()
}
