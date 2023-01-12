package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/net/proxy"
)

var (
	socks    = flag.String("socks", "", "Upstream socks5 proxy, e.g. 127.0.0.1:1080")
	socksUds = flag.String("socks-uds", "", "Path of unix domain socket for upstream socks5 proxy")
	bind     = flag.String("bind", "0.0.0.0:8765", "Address to bind")
	debug    = flag.Bool("debug", false, "Verbose logs")
)

const (
	optPrefix        = "urlproxyOpt"
	optHeader        = optPrefix + "Header"
	optSchema        = optPrefix + "Schema"
	optSocks         = optPrefix + "Socks"
	optDns           = optPrefix + "Dns"
	optIp            = optPrefix + "Ip"
	optTimeoutMs     = optPrefix + "TimeoutMs"
	optRetriesNon2xx = optPrefix + "RetriesNon2xx"
	optRetriesError  = optPrefix + "RetriesError"
	optAntiCaching   = optPrefix + "AntiCaching"
	optRaceMode      = optPrefix + "RaceMode"
)

const (
	maxParallelism = 5
)

var (
	// these headers may conflict with the behavior of http client
	donotForwardToReq = map[string]bool{
		"Host":                true,
		"Accept-Encoding":     true,
		"Proxy-Connection":    true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
	}

	// these headers may conflict with the behavior of http responser
	donotForwardToResp = map[string]bool{
		"Content-Length":    true,
		"Content-Encoding":  true,
		"Transfer-Encoding": true,
	}

	headerOrigin = http.CanonicalHeaderKey("X-Urlproxy-Origin")
)

var (
	instUUID   = uuid.NewString()
	clientPool = sync.Map{}
)

type connEx struct {
	*net.TCPConn
	bufrd *bufio.Reader
}

func (c *connEx) Read(p []byte) (n int, err error) {
	if c.bufrd != nil {
		return c.bufrd.Read(p)
	}
	return c.TCPConn.Read(p)
}

type dialer interface {
	Dial(network, addr string) (c net.Conn, err error)
	DialContext(ctx context.Context, network, addr string) (c net.Conn, err error)
}

func getDialer(opts url.Values) (dialer, string, bool) {
	var identifier string
	var direct dialer = &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if opts.Has(optDns) {
		dns := opts.Get(optDns)
		identifier += "[dns:" + dns + "]"
		direct.(*net.Dialer).Resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "udp", dns)
			},
		}
	}

	socksAddr := *socks
	if opts.Has(optSocks) {
		socksAddr = opts.Get(optSocks)
		if socksAddr == "" || socksAddr == "off" {
			// optSocks == "" or "off" means user wants to disable socks proxying
			return direct, identifier, false
		}
	}
	pd := proxy.FromEnvironmentUsing(direct)
	if socksAddr != "" {
		identifier += "[socks:" + socksAddr + "]"
		pd, _ = proxy.SOCKS5("tcp", socksAddr, nil, direct)
	} else if *socksUds != "" {
		identifier += "[socks-uds:" + *socksUds + "]"
		pd, _ = proxy.SOCKS5("unix", *socksUds, nil, direct)
	}
	if pd == nil {
		return direct, identifier, false
	} else {
		return pd.(dialer), identifier, pd.(dialer) != direct
	}
}

func getHttpCli(opts url.Values, reusable bool) *http.Client {
	d, identifier, socksed := getDialer(opts)
	if cli, ok := clientPool.Load(identifier); ok && reusable {
		return cli.(*http.Client)
	}
	// same as http.DefaultTransport
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if socksed {
		transport.Proxy = nil // uses dialer's proxy
	}
	cli := &http.Client{Transport: transport}
	if reusable {
		clientPool.Store(identifier, cli)
	}
	return cli
}

func forward(from, to *connEx, wg *sync.WaitGroup) {
	io.Copy(to, from)
	from.CloseRead()
	to.CloseWrite()
	wg.Done()
}

func int32Value(v string) (int, bool) {
	i, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return int(i), true
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet ||
		method == http.MethodHead ||
		method == http.MethodOptions ||
		method == http.MethodTrace
}

func goodStatusCode(statusCode int) bool {
	return statusCode >= 100 && statusCode < 400
}

func prepareProxyRequest(req *http.Request) (proxyReq *http.Request, opts url.Values, err error) {
	reqSign := instUUID + "|" + req.URL.String()
	for _, origin := range req.Header[headerOrigin] {
		if origin == reqSign {
			err = fmt.Errorf("recursively request, sign: %s", reqSign)
			return
		}
	}

	proxyUrl := *req.URL
	query := proxyUrl.Query()
	opts = url.Values{}
	for k, v := range query {
		if strings.HasPrefix(k, optPrefix) {
			delete(query, k)
			opts[k] = v
		}
	}
	proxyUrl.RawQuery = query.Encode()

	if req.URL.Scheme != "" {
		// it's a regular http proxy request if Scheme is not empty
	} else {
		path := proxyUrl.EscapedPath()
		var filtered []string
		for _, seg := range strings.Split(path, "/") {
			if seg == "" {
				continue
			}
			if strings.HasPrefix(seg, "urlproxyOpt") {
				parts := append(strings.Split(seg, "="), "")
				opts.Add(parts[0], parts[1])
				continue
			}
			filtered = append(filtered, seg)
		}

		if len(filtered) < 1 {
			err = fmt.Errorf("path should contain target host")
			return
		}
		targetHost := filtered[0]
		proxyUrl.Host = targetHost
		proxyUrl.Path = "/" + strings.Join(filtered[1:], "/")
		proxyUrl.Scheme = "http"
		if opts.Has(optSchema) {
			proxyUrl.Scheme = opts.Get(optSchema)
		}
	}

	host := proxyUrl.Host
	if opts.Has(optIp) {
		port := proxyUrl.Port()
		if port != "" {
			proxyUrl.Host = opts.Get(optIp) + ":" + port
		} else {
			proxyUrl.Host = opts.Get(optIp)
		}
	}

	proxyReq, err = http.NewRequestWithContext(
		req.Context(), req.Method, proxyUrl.String(), req.Body)
	if err != nil {
		return
	}

	// force set Host
	proxyReq.Host = host

	// forward headers
	for k := range req.Header {
		if _, exists := donotForwardToReq[k]; exists {
			continue
		}
		proxyReq.Header[k] = append(proxyReq.Header[k], req.Header[k]...)
	}

	// add origin header to break recursive requests
	proxyReq.Header.Add(headerOrigin, reqSign)

	// add custom headers
	for _, header := range opts[optHeader] {
		if header == "" {
			continue
		}
		parts := append(strings.SplitN(header, ":", 2), "")
		proxyReq.Header.Set(parts[0], strings.TrimLeftFunc(parts[1], unicode.IsSpace))
	}

	if *debug {
		log.Printf("[DBG] proxyReq: %+v", proxyReq)
	}

	return
}

func doRequest(proxyReq *http.Request, opts url.Values) (*http.Response, error) {
	parallelism, _ := int32Value(opts.Get(optRaceMode))
	if parallelism > maxParallelism {
		parallelism = maxParallelism
	}
	if parallelism > 1 && isSafeMethod(proxyReq.Method) {
		type result struct {
			resp *http.Response
			err  error
			idx  int
		}
		ch := make(chan result, parallelism)
		var cancels []context.CancelFunc
		for i := 0; i < parallelism; i++ {
			cli := getHttpCli(opts, false)
			ctx, cancel := context.WithCancel(proxyReq.Context())
			req := proxyReq.WithContext(ctx)
			cancels = append(cancels, cancel)
			go func(i int) {
				if *debug {
					log.Printf("[DBG] [RACE] doing concurrent request, idx: %d", i)
				}
				resp, err := doRequestSerial(cli, req, opts)
				ch <- result{resp, err, i}
			}(i)
		}
		var lastResp *http.Response
		var lastErr error
		var lastIdx int = -1
		defer func() {
			for i, c := range cancels {
				if lastIdx != i {
					c()
				}
			}
		}()
		count := parallelism
		for count > 0 {
			select {
			case <-proxyReq.Context().Done():
				log.Printf("[ERR] [RACE] request context done, url: %s, err: %s",
					proxyReq.URL.String(), proxyReq.Context().Err())
				lastErr = proxyReq.Context().Err()
				count = 0 // break for loop
			case r := <-ch:
				lastResp = r.resp
				lastErr = r.err
				lastIdx = r.idx
				if r.err == nil && goodStatusCode(r.resp.StatusCode) {
					log.Printf("[DBG] [RACE] got final response")
					return r.resp, r.err
				}
				if *debug {
					statusCode := 0
					if r.resp != nil {
						statusCode = r.resp.StatusCode
					}
					log.Printf("[DBG] [RACE] got bad response, status code: %d, err: %v",
						statusCode, r.err)
				}
				count--
			}
		}
		return lastResp, lastErr
	} else {
		cli := getHttpCli(opts, true)
		return doRequestSerial(cli, proxyReq, opts)
	}
}

func doRequestSerial(cli *http.Client, proxyReq *http.Request, opts url.Values) (*http.Response, error) {
	retriesNon2xx, _ := int32Value(opts.Get(optRetriesNon2xx))
	retriesError, _ := int32Value(opts.Get(optRetriesError))

	const maxRetryDelay = time.Second
	retryDelay := 100 * time.Millisecond
	raiseRetryDelay := func() {
		retryDelay = 2 * retryDelay
		if retryDelay > maxRetryDelay {
			retryDelay = maxRetryDelay
		}
	}

	for {
		if opts.Has(optAntiCaching) {
			query := proxyReq.URL.Query()
			query.Set("__t", strconv.FormatInt(time.Now().UnixNano(), 10))
			proxyReq.URL.RawQuery = query.Encode()
		}

		resp, err := cli.Do(proxyReq)
		if err != nil {
			log.Printf("[ERR] do request failed, url: %s, err: %s", proxyReq.URL.String(), err)
			if retriesError == 0 {
				return nil, err
			}
			if *debug {
				log.Printf("[DBG] url: %s, err: %s. retry for errors, remaining retries: %d",
					proxyReq.URL.String(), err, retriesError)
			}
			retriesError--
			time.Sleep(retryDelay)
			raiseRetryDelay()
			continue
		}
		if goodStatusCode(resp.StatusCode) {
			// success
			return resp, nil
		} else {
			// retry non-2xx only for GET, HEAD, OPTIONS and TRACE.
			// there maybe a request body for other methods, but the body
			// object from the original request has been closed by the last
			// time of requesting.
			if retriesNon2xx == 0 || !isSafeMethod(proxyReq.Method) {
				return resp, nil
			}
			if *debug {
				log.Printf("[DBG] url: %s, status code: %d. retry for non-2xx, remaining retries: %d",
					proxyReq.URL.String(), resp.StatusCode, retriesNon2xx)
			}
			if _, err := io.Copy(ioutil.Discard, resp.Body); err != nil {
				log.Printf("[ERR] discard response body failed, err: %s", err)
				return resp, err
			}
			resp.Body.Close()
			retriesNon2xx--
			time.Sleep(retryDelay)
			raiseRetryDelay()
			continue
		}
	}
}

func forwardResponse(w http.ResponseWriter, proxyResp *http.Response) {
	for k := range proxyResp.Header {
		if _, exists := donotForwardToResp[k]; exists {
			continue
		}
		w.Header().Set(k, proxyResp.Header.Get(k))
	}
	w.WriteHeader(proxyResp.StatusCode)
	_, err := io.Copy(w, proxyResp.Body)
	proxyResp.Body.Close()
	if err != nil {
		return
	}
	for k := range proxyResp.Trailer {
		w.Header().Set(k, proxyResp.Trailer.Get(k))
	}
}

func handleConnectMethod(w http.ResponseWriter, req *http.Request) {
	// there is no parameter or path for CONNECT request,
	// so empty options just fine.
	opts := url.Values{}
	d, _, _ := getDialer(opts)
	conn, err := d.DialContext(req.Context(), "tcp", req.URL.Host)
	if err != nil {
		log.Printf("[ERR] dial to %s failed, err: %s", req.URL.Host, err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(err.Error()))
		return
	}
	defer conn.Close()
	inConn, bufrw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Printf("[ERR] hijack failed, err: %s", err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(err.Error()))
		return
	}
	defer inConn.Close()

	resp := http.Response{
		StatusCode: http.StatusOK,
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
	resp.Write(inConn)

	if _, ok := conn.(*net.TCPConn); !ok {
		// assuming it's a socks.Conn object,
		// it doesn't implement CloseRead()/CloseWrite().
		// overcome by using its underlay net.TCPConn.
		// ref: https://pkg.go.dev/golang.org/x/net@v0.5.0/internal/socks#Conn
		conn = reflect.ValueOf(conn).Elem().FieldByName("Conn").Interface().(net.Conn)
	}

	conn1 := &connEx{TCPConn: conn.(*net.TCPConn)}
	conn2 := &connEx{TCPConn: inConn.(*net.TCPConn), bufrd: bufrw.Reader}
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go forward(conn1, conn2, wg)
	go forward(conn2, conn1, wg)
	wg.Wait()

}

func httpProxyHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodConnect {
		handleConnectMethod(w, req)
		return
	}

	proxyReq, opts, err := prepareProxyRequest(req)
	if err != nil {
		log.Printf("[ERR] prepare proxy request failed, err: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	if timeoutMs, ok := int32Value(opts.Get(optTimeoutMs)); ok {
		var ctx context.Context
		ctx, cancel := context.WithTimeout(req.Context(), time.Duration(timeoutMs)*time.Millisecond)
		proxyReq = proxyReq.WithContext(ctx)
		defer cancel()
	}

	proxyResp, err := doRequest(proxyReq, opts)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(err.Error()))
		return
	}
	forwardResponse(w, proxyResp)
}

func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags | log.Lmicroseconds)
	flag.Parse()
	ln, err := net.Listen("tcp", *bind)
	if err != nil {
		log.Fatalf("[FATAL] listen to %s failed, err: %v", *bind, err)
		return
	}
	log.Printf("[INF] listen to %s", ln.Addr().String())
	http.Serve(ln, http.HandlerFunc(httpProxyHandler))
	log.Printf("[INF] exit")
}
