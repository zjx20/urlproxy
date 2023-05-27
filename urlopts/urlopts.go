package urlopts

import (
	"net/url"
	"strings"
	"sync"

	"github.com/zjx20/urlproxy/logger"
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

func NewOption(name string) Option {
	o := options[name]
	if o == nil {
		return nil
	}
	return o.Clone()
}

func conv(uopts url.Values) *Options {
	opts := &Options{}
	for k, values := range uopts {
		opt := NewOption(k)
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
			opts.optMap.Store(k, opt)
		}
	}
	return opts
}

func ToList(opts *Options) []string {
	var result []string
	opts.optMap.Range(func(key, value any) bool {
		opt := value.(Option)
		if !opt.IsPresent() {
			return true
		}
		result = append(result, opt.ToUrlOption()...)
		return true
	})
	return result
}

func Extract(u *url.URL) (after url.URL, opts *Options) {
	after = *u
	query := after.Query()
	uopts := url.Values{}
	for k, v := range query {
		if strings.HasPrefix(k, UrlOptionPrefix) {
			delete(query, k)
			uopts[k] = v
		}
	}
	after.RawQuery = query.Encode()

	path := after.RawPath
	if path == "" {
		path = after.Path
	}
	var filtered []string
	for _, seg := range strings.Split(path, "/") {
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
				uopts.Add(seg[:pos], seg[next:])
				continue
			}
		}
		filtered = append(filtered, seg)
	}
	// extract the first segment of the path as the host.
	// but if u.Scheme != "", the url is for a regular http proxy request,
	// so it's not a urlproxied url.
	if !uopts.Has(OptHost.name) && u.Scheme == "" {
		if !uopts.Has(OptScheme.name) || uopts.Get(OptScheme.name) != "file" {
			if len(filtered) > 0 {
				host := filtered[0]
				filtered = filtered[1:]
				uopts.Set(OptHost.name, host)
			}
		}
	}
	after.Path = "/" + strings.Join(filtered, "/")

	opts = conv(uopts)
	return
}
