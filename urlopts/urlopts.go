package urlopts

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/zjx20/urlproxy/logger"
)

const (
	optPrefix         = "uOpt"
	optHost           = optPrefix + "Host"
	optHeader         = optPrefix + "Header"
	optScheme         = optPrefix + "Scheme"
	optSocks          = optPrefix + "Socks"
	optDns            = optPrefix + "Dns"
	optIp             = optPrefix + "Ip"
	optTimeoutMs      = optPrefix + "TimeoutMs"
	optRetriesNon2xx  = optPrefix + "RetriesNon2xx"
	optRetriesError   = optPrefix + "RetriesError"
	optAntiCaching    = optPrefix + "AntiCaching"
	optRaceMode       = optPrefix + "RaceMode"
	optHLSBoost       = optPrefix + "HLSBoost"
	optHLSPlaylist    = optPrefix + "HLSPlaylist"
	optHLSUser        = optPrefix + "HLSUser"
	optHLSSegment     = optPrefix + "HLSSegment"
	optHLSSkip        = optPrefix + "HLSSkip"
	optHLSPrefetches  = optPrefix + "HLSPrefetches"
	optAntPieceSize   = optPrefix + "AntPieceSize"
	optAntConcurrency = optPrefix + "AntConcurrency"
)

type Options struct {
	Host                *string
	Headers             []string
	Scheme              *string
	Socks               *string
	Dns                 *string
	Ip                  *string
	TimeoutMs           *int
	RetriesNon2xx       *int
	RetriesError        *int
	AntiCaching         *bool
	RaceMode            *int
	HLSBoost            *bool
	HLSPlaylist         *string
	HLSUser             *string
	HLSSegment          *string
	HLSSkip             *bool
	HLSPrefetches       *int
	AntPieceSize        *int
	AntConcurrentPieces *int
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
		case optHLSBoost:
			setBoolPtr(&opts.HLSBoost, uopts.Get(k))
		case optHLSPlaylist:
			opts.HLSPlaylist = ptr(uopts.Get(k))
		case optHLSUser:
			opts.HLSUser = ptr(uopts.Get(k))
		case optHLSSegment:
			opts.HLSSegment = ptr(uopts.Get(k))
		case optHLSSkip:
			setBoolPtr(&opts.HLSSkip, uopts.Get(k))
		case optHLSPrefetches:
			trySetIntPtr(&opts.HLSPrefetches, uopts.Get(k))
		case optAntPieceSize:
			trySetIntPtr(&opts.AntPieceSize, uopts.Get(k))
		case optAntConcurrency:
			trySetIntPtr(&opts.AntConcurrentPieces, uopts.Get(k))
		}
	}
	return opts
}

func ToList(opts *Options) []string {
	var result []string
	if opts.Host != nil {
		result = append(result, item(optHost, *opts.Host))
	}
	if len(opts.Headers) > 0 {
		for _, h := range opts.Headers {
			result = append(result, item(optHeader, h))
		}
	}
	if opts.Scheme != nil {
		result = append(result, item(optScheme, *opts.Scheme))
	}
	if opts.Socks != nil {
		result = append(result, item(optSocks, *opts.Socks))
	}
	if opts.Dns != nil {
		result = append(result, item(optDns, *opts.Dns))
	}
	if opts.Ip != nil {
		result = append(result, item(optIp, *opts.Ip))
	}
	if opts.TimeoutMs != nil {
		result = append(result, item(optTimeoutMs, strconv.FormatInt(int64(*opts.TimeoutMs), 10)))
	}
	if opts.RetriesNon2xx != nil {
		result = append(result, item(optRetriesNon2xx, strconv.FormatInt(int64(*opts.RetriesNon2xx), 10)))
	}
	if opts.RetriesError != nil {
		result = append(result, item(optRetriesError, strconv.FormatInt(int64(*opts.RetriesError), 10)))
	}
	if opts.AntiCaching != nil {
		result = append(result, item(optAntiCaching, strconv.FormatBool(*opts.AntiCaching)))
	}
	if opts.RaceMode != nil {
		result = append(result, item(optRaceMode, strconv.FormatInt(int64(*opts.RaceMode), 10)))
	}
	if opts.HLSBoost != nil {
		result = append(result, item(optHLSBoost, strconv.FormatBool(*opts.HLSBoost)))
	}
	if opts.HLSPlaylist != nil {
		result = append(result, item(optHLSPlaylist, *opts.HLSPlaylist))
	}
	if opts.HLSUser != nil {
		result = append(result, item(optHLSUser, *opts.HLSUser))
	}
	if opts.HLSSegment != nil {
		result = append(result, item(optHLSSegment, *opts.HLSSegment))
	}
	if opts.HLSSkip != nil {
		result = append(result, item(optHLSSkip, strconv.FormatBool(*opts.HLSSkip)))
	}
	if opts.HLSPrefetches != nil {
		result = append(result, item(optHLSPrefetches, strconv.FormatInt(int64(*opts.HLSPrefetches), 10)))
	}
	if opts.AntPieceSize != nil {
		result = append(result, item(optAntPieceSize, strconv.FormatInt(int64(*opts.AntPieceSize), 10)))
	}
	if opts.AntConcurrentPieces != nil {
		result = append(result, item(optAntConcurrency, strconv.FormatInt(int64(*opts.AntConcurrentPieces), 10)))
	}
	return result
}

func item(k, v string) string {
	return k + "=" + v
}

func Extract(u *url.URL) (after url.URL, opts *Options) {
	after = *u
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
	// extract the first segment of the path as the host.
	// but if u.Scheme != "", the url is for a regular http proxy request,
	// so it's not a urlproxied url.
	if !uopts.Has(optHost) && u.Scheme == "" {
		if !uopts.Has(optScheme) || uopts.Get(optScheme) != "file" {
			if len(filtered) > 0 {
				host := filtered[0]
				filtered = filtered[1:]
				uopts.Set(optHost, host)
			}
		}
	}
	after.Path = "/" + strings.Join(filtered, "/")

	opts = conv(uopts)
	return
}

func ToVal[T int | bool](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func ptr(v string) *string {
	return &v
}

func trySetIntPtr(vp **int, s string) {
	if v, err := convToInt(s); err == nil {
		*vp = &v
	} else {
		logger.Warnf("invalid int value: %s", s)
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
