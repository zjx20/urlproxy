package urlopts

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/zjx20/urlproxy/logger"
)

var (
	queryParamRegexp = regexp.MustCompile(fmt.Sprintf("(^|&)((%s[^=]*)=([^&]*))", UrlOptionPrefix))
)

type Options struct {
	optMap sync.Map // name => Option
}

func (opts *Options) Set(opt Option) {
	opts.optMap.Store(opt.Name(), opt)
}

func (opts *Options) Remove(id interface{ Name() string }) {
	opts.optMap.Delete(id.Name())
}

func (opts *Options) Clone() *Options {
	clone := &Options{}
	opts.optMap.Range(func(key, value any) bool {
		clone.optMap.Store(key, value.(Option).Clone())
		return true
	})
	return clone
}

func (opts *Options) String() string {
	sb := strings.Builder{}
	sb.WriteString("UrlOptions[")
	first := true
	opts.optMap.Range(func(key, value any) bool {
		o := value.(Option)
		if !o.IsPresent() {
			return true
		}
		items := o.ToUrlOption()
		for _, item := range items {
			if !first {
				sb.WriteRune(',')
			}
			first = false
			sb.WriteString(item)
		}
		return true
	})
	sb.WriteRune(']')
	return sb.String()
}

func newOption(name string) Option {
	o := options[name]
	if o == nil {
		return nil
	}
	return o.Clone()
}

func conv(uopts url.Values) *Options {
	opts := &Options{}
	for k, values := range uopts {
		opt := newOption(k)
		if opt == nil {
			logger.Warnf("unknown option %s", k)
			continue
		}
		ok := true
		for _, value := range values {
			err := opt.Parse(value)
			if err != nil {
				logger.Errorf("parse option %s failed, input: %s, error: %s",
					k, value, err)
				ok = false
				break
			}
		}
		if ok {
			opts.Set(opt)
		}
	}
	return opts
}

func extractOptionName(s string) (bool, string) {
	if strings.HasPrefix(s, UrlOptionPrefix) {
		return true, s[len(UrlOptionPrefix):]
	}
	return false, s
}

func pathUnescaped(s string) string {
	if unescaped, err := url.PathUnescape(s); err == nil {
		return unescaped
	}
	return s
}

func setRawPath(u *url.URL, rawPath string) {
	u.RawPath = rawPath
	u.Path = pathUnescaped(rawPath)
}

func Extract(u *url.URL) (after url.URL, opts *Options) {
	// Some of the http servers have strict request verification, even the
	// order of query parameters is not allowed to change. So we can't extract
	// url options by using u.Query() and query.Encode(), which can change the
	// order of the query parameters.
	uopts := url.Values{}
	rawQuery := u.RawQuery
	matches := queryParamRegexp.FindAllStringSubmatch(rawQuery, -1)
	for _, match := range matches {
		k := match[3]
		v := match[4]
		rm := match[0]
		if ok, oName := extractOptionName(k); ok {
			uopts.Add(oName, v)
		}
		rawQuery = strings.ReplaceAll(rawQuery, rm, "")
	}
	// hack: the regexp does not capture the "&" at the right side,
	//       it's possible to leave a "&" at the front.
	//       e.g. input "uOptHost=myhost&foo=bar", after processing: "&foo=bar"
	rawQuery = strings.TrimPrefix(rawQuery, "&")

	rawPath := u.EscapedPath()
	var filtered []string
	for _, seg := range strings.Split(rawPath, "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, UrlOptionPrefix) {
			pos := strings.Index(seg, "=")
			next := pos + 1
			if pos == -1 {
				pos = strings.Index(seg, "%3D")
				next = pos + 3
				if pos == -1 {
					pos = strings.Index(seg, "%3d")
					next = pos + 3
				}
			}
			if pos != -1 {
				_, oName := extractOptionName(seg[:pos])
				uopts.Add(oName, pathUnescaped(seg[next:]))
				continue
			}
		}
		filtered = append(filtered, seg)
	}
	// extract the first segment of the path as the host.
	// but if u.Scheme != "", the url is for a regular http proxy request,
	// so it's not a urlproxied url.
	if !uopts.Has(OptHost.name) && u.Scheme == "" {
		scheme := strings.ToLower(uopts.Get(OptScheme.name))
		if scheme == "" || scheme == "http" || scheme == "https" {
			if len(filtered) > 0 {
				host := filtered[0]
				filtered = filtered[1:]
				uopts.Set(OptHost.name, pathUnescaped(host))
			}
		}
	}
	rawPath = strings.Join(filtered, "/")
	if strings.HasPrefix(u.Path, "/") {
		rawPath = "/" + rawPath
	}

	after = *u
	after.RawQuery = rawQuery
	setRawPath(&after, rawPath)
	opts = conv(uopts)
	return
}

func ToList(opts *Options) []string {
	var result []string
	opts.optMap.Range(func(key, value any) bool {
		opt := value.(Option)
		if !opt.IsPresent() {
			return true
		}
		// note: ToUrlOption() is already escaped by url.PathEscape()
		result = append(result, opt.ToUrlOption()...)
		return true
	})
	return result
}

func SortedOptionPath(opts *Options) string {
	list := ToList(opts)
	sort.Strings(list)
	return strings.Join(list, "/")
}

func RelocateToUrlproxy(u *url.URL, opts *Options) *url.URL {
	cloneOpts := opts.Clone()

	if u.Scheme != "" {
		// it's an absolute url, convert it into a relative url for urlproxy
		cloneOpts.Set(OptScheme.New(u.Scheme))
		cloneOpts.Set(OptHost.New(u.Host))
		optRawPath := SortedOptionPath(cloneOpts)
		rawPath := "/" + optRawPath + u.EscapedPath()
		setRawPath(u, rawPath)
		u.Scheme = ""
		u.Host = ""
	} else {
		// it's a relative url
		optRawPath := SortedOptionPath(cloneOpts)
		if optRawPath != "" {
			if strings.HasPrefix(u.Path, "/") {
				// absolute path
				rawPath := "/" + optRawPath + u.EscapedPath()
				setRawPath(u, rawPath)
			} else {
				// relative path
				rawPath := optRawPath + "/" + u.EscapedPath()
				setRawPath(u, rawPath)
			}
		}
	}
	return u
}
