package hlsboost

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/zjx20/urlproxy/urlopts"
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
	tmp := opts.Clone()
	tmp.Set(urlopts.OptHLSSkip.New(true))
	path := toUrlproxyURI(relativeToPath, uri, tmp)
	return fmt.Sprintf("%s://%s%s", h.scheme, h.addr, path)
}

func sortedOptionPath(opts *urlopts.Options) string {
	list := urlopts.ToList(opts)
	sort.Strings(list)
	return strings.Join(list, "/")
}

func toUrlproxyURI(relativeToPath string, uri string, opts *urlopts.Options) string {
	if uri == "" {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	cloneOpts := opts.Clone()

	if u.Scheme != "" {
		// it's an absolute url, convert it into a relative url for urlproxy
		cloneOpts.Set(urlopts.OptScheme.New(u.Scheme))
		cloneOpts.Set(urlopts.OptHost.New(u.Host))
		optPath := sortedOptionPath(cloneOpts)
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
		optPath := sortedOptionPath(cloneOpts)
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
