package urlopts

import (
	"net/url"
	"strconv"
	"strings"
)

const (
	optPrefix        = "urlproxyOpt"
	optHost          = optPrefix + "Host"
	optHeader        = optPrefix + "Header"
	optScheme        = optPrefix + "Scheme"
	optSocks         = optPrefix + "Socks"
	optDns           = optPrefix + "Dns"
	optIp            = optPrefix + "Ip"
	optTimeoutMs     = optPrefix + "TimeoutMs"
	optRetriesNon2xx = optPrefix + "RetriesNon2xx"
	optRetriesError  = optPrefix + "RetriesError"
	optAntiCaching   = optPrefix + "AntiCaching"
	optRaceMode      = optPrefix + "RaceMode"
)

type Options struct {
	Host          *string
	Headers       []string
	Scheme        *string
	Socks         *string
	Dns           *string
	Ip            *string
	TimeoutMs     *int
	RetriesNon2xx *int
	RetriesError  *int
	AntiCaching   *bool
	RaceMode      *int
}

func conv(uopts url.Values) *Options {
	opts := &Options{}
	for k := range uopts {
		switch k {
		case optHost:
			opts.Host = ptr(uopts.Get(k))
		case optHeader:
			opts.Headers = uopts[k]
		case optScheme:
			opts.Scheme = ptr(uopts.Get(k))
		case optSocks:
			opts.Socks = ptr(uopts.Get(k))
		case optDns:
			opts.Dns = ptr(uopts.Get(k))
		case optIp:
			opts.Ip = ptr(uopts.Get(k))
		case optTimeoutMs:
			trySetIntPtr(&opts.TimeoutMs, uopts.Get(k))
		case optRetriesNon2xx:
			trySetIntPtr(&opts.RetriesNon2xx, uopts.Get(k))
		case optRetriesError:
			trySetIntPtr(&opts.RetriesError, uopts.Get(k))
		case optAntiCaching:
			setBoolPtr(&opts.AntiCaching, uopts.Get(k))
		case optRaceMode:
			trySetIntPtr(&opts.RaceMode, uopts.Get(k))
		}
	}
	return opts
}

func Extract(u *url.URL) (after *url.URL, opts *Options) {
	tmp := *u
	after = &tmp
	query := after.Query()
	uopts := url.Values{}
	for k, v := range query {
		if strings.HasPrefix(k, optPrefix) {
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
		if strings.HasPrefix(seg, optPrefix) {
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
	after.Path = "/" + strings.Join(filtered, "/")

	opts = conv(uopts)
	return
}

func ptr[T string | int](v T) *T {
	return &v
}

func trySetIntPtr(vp **int, s string) {
	if v, err := convToInt(s); err == nil {
		*vp = &v
	}
}

func convToInt(s string) (int, error) {
	if i, err := strconv.ParseInt(s, 10, 64); err != nil {
		return 0, err
	} else {
		return int(i), err
	}
}

var (
	trueVals = map[string]bool{
		"true": true,
		"yes":  true,
		"on":   true,
		"1":    true,
	}
)

func setBoolPtr(vp **bool, s string) {
	v := trueVals[strings.ToLower(s)]
	*vp = &v
}
