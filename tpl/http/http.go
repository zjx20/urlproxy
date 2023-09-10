package http

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/zjx20/urlproxy/urlopts"
)

func Funcs() template.FuncMap {
	return template.FuncMap{
		"httpReq":       httpReq,
		"httpHeader":    httpHeader,
		"parseUrl":      parseUrl,
		"urlproxiedUrl": urlproxiedUrl,
	}
}

func httpReq(method string, url string, body string,
	header *HeaderWrapper, timeoutSec int) (*ResponseWrapper, error) {
	timeout := time.Duration(timeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r := strings.NewReader(body)
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	if header != nil {
		for k, v := range header.Header {
			req.Header[k] = v
		}
	}
	resp, err := http.DefaultClient.Do(req)
	return &ResponseWrapper{resp}, err
}

func httpHeader(arr ...string) *HeaderWrapper {
	ret := make(http.Header)
	size := len(arr)
	if size%2 == 1 {
		size--
	}
	for i := 0; i < size; i += 2 {
		ret.Add(arr[i], arr[i+1])
	}
	return &HeaderWrapper{ret}
}

func parseUrl(u string) (*url.URL, error) {
	return url.Parse(u)
}

func urlproxiedUrl(originalUrl string, urlproxyBaseUrl string, opts ...string) (string, error) {
	if urlproxyBaseUrl == "" {
		return "", fmt.Errorf("empty urlproxyBaseUrl")
	}
	regex := regexp.MustCompile("([a-zA-Z]+)://(.*)")
	matches := regex.FindStringSubmatch(originalUrl)
	if len(matches) == 0 {
		return "", fmt.Errorf("invalid url: %s", originalUrl)
	}
	if !strings.HasSuffix(urlproxyBaseUrl, "/") {
		urlproxyBaseUrl += "/"
	}
	u, err := url.Parse(urlproxyBaseUrl + matches[2])
	if err != nil {
		return "", err
	}

	q := u.Query()
	size := len(opts)
	if size%2 == 1 {
		size--
	}
	for i := 0; i < size; i += 2 {
		q.Add(opts[i], opts[i+1])
	}
	if !q.Has(urlopts.OptScheme.OptionKey()) {
		q.Set(urlopts.OptScheme.OptionKey(), matches[1])
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
