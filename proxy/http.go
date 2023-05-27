package proxy

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zjx20/urlproxy/logger"
	"github.com/zjx20/urlproxy/urlopts"
	"golang.org/x/net/proxy"
)

var (
	socks    = flag.String("socks", "", "Upstream socks5 proxy, e.g. 127.0.0.1:1080")
	socksUds = flag.String("socks-uds", "", "Path of unix domain socket for upstream socks5 proxy")
	fileRoot = flag.String("file-root", "", "Root path for the file scheme")
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

type dialCtxFunc func(ctx context.Context, network, addr string) (c net.Conn, err error)

func getResolvedAddr(ctx context.Context, dns string, addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		logger.Errorf("bad addr %s", addr)
		return addr
	}
	if ip := net.ParseIP(addr); ip != nil {
		return addr
	}
	addrs, err := (&net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", dns)
		},
	}).LookupHost(ctx, host)
	if err != nil {
		logger.Errorf("resolve via custom DNS(%s) failed, err: %s", dns, err)
	} else {
		if len(addrs) == 0 {
			logger.Errorf("resolve result for host(%s) is empty", host)
		} else {
			logger.Debugf("resolve results for host(%s): %q", host, addrs)
			return net.JoinHostPort(addrs[0], port)
		}
	}
	return addr
}

func getDialer(host string, opts *urlopts.Options) (dialCtxFunc, string) {
	var identifier string

	var pd proxy.Dialer
	if socksAddr, ok := urlopts.OptSocks.ValueFrom(opts); ok {
		if socksAddr == "" || socksAddr == "off" {
			// socksAddr == "" or "off" means user wants to disable socks proxying
		} else {
			identifier += "[socks:" + socksAddr + "]"
			pd, _ = proxy.SOCKS5("tcp", socksAddr, nil, nil)
		}
	} else {
		if *socks != "" {
			identifier += "[socks:" + *socks + "]"
			pd, _ = proxy.SOCKS5("tcp", *socks, nil, nil)
		} else if *socksUds != "" {
			identifier += "[socks-uds:" + *socksUds + "]"
			pd, _ = proxy.SOCKS5("unix", *socksUds, nil, nil)
		}
	}

	var fn dialCtxFunc
	if pd == nil {
		d := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		fn = d.DialContext
	} else {
		fn = pd.(dialer).DialContext
	}

	if dns, ok := urlopts.OptDns.ValueFrom(opts); ok {
		identifier += "[dns:" + dns + "]"
		prevFn := fn
		fn = func(ctx context.Context, network, addr string) (c net.Conn, err error) {
			finalAddr := getResolvedAddr(ctx, dns, addr)
			return prevFn(ctx, network, finalAddr)
		}
	}

	if ip, ok := urlopts.OptIp.ValueFrom(opts); ok {
		identifier += "[ip:" + host + ":" + ip + "]"
		prevFn := fn
		fn = func(ctx context.Context, network, addr string) (c net.Conn, err error) {
			finalAddr := addr
			hostFromAddr := addr[:strings.LastIndex(addr, ":")]
			if addr == host || hostFromAddr == host {
				finalAddr = ip + addr[strings.LastIndex(addr, ":"):]
				logger.Infof("resolved %s to %s", addr, finalAddr)
			}
			return prevFn(ctx, network, finalAddr)
		}
	}

	return fn, identifier
}

func getHttpCli(host string, opts *urlopts.Options, reusable bool) *http.Client {
	dialCtxFn, identifier := getDialer(host, opts)
	if cli, ok := clientPool.Load(identifier); ok && reusable {
		return cli.(*http.Client)
	}
	// same as http.DefaultTransport
	transport := &http.Transport{
		DialContext:           dialCtxFn,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if *fileRoot != "" {
		transport.RegisterProtocol("file", http.NewFileTransport(http.Dir(*fileRoot)))
	}
	cli := &http.Client{
		Transport: transport,
		// no redirect
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
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

func isSafeMethod(method string) bool {
	return method == http.MethodGet ||
		method == http.MethodHead ||
		method == http.MethodOptions ||
		method == http.MethodTrace
}

func goodStatusCode(statusCode int) bool {
	return statusCode >= 100 && statusCode < 400
}

func prepareProxyRequest(req *http.Request, opts *urlopts.Options) (proxyReq *http.Request, err error) {
	reqSign := instUUID + "|" + req.URL.String()
	for _, origin := range req.Header[headerOrigin] {
		if origin == reqSign {
			err = fmt.Errorf("recursively request, sign: %s", reqSign)
			return
		}
	}
	proxyReqUrl := *req.URL

	if proxyReqUrl.Scheme != "" {
		// it's a regular http proxy request if Scheme is not empty
	} else {
		// update the scheme
		proxyReqUrl.Scheme = "http"
		if scheme, ok := urlopts.OptScheme.ValueFrom(opts); ok {
			proxyReqUrl.Scheme = scheme
		}
		// update the host
		if proxyReqUrl.Scheme != "file" {
			if host, ok := urlopts.OptHost.ValueFrom(opts); ok {
				proxyReqUrl.Host = host
			} else {
				err = fmt.Errorf("OptHost not exists")
				return
			}
		}
	}

	proxyReq, err = http.NewRequestWithContext(
		req.Context(), req.Method, proxyReqUrl.String(), req.Body)
	if err != nil {
		return
	}

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
	headers, _ := urlopts.OptHeader.ValueFrom(opts)
	for key, values := range headers {
		proxyReq.Header[key] = values
	}

	logger.Debugf("proxyReq: %+v", proxyReq)

	return
}

func doRequest(proxyReq *http.Request, opts *urlopts.Options) (*http.Response, error) {
	parallelism, _ := urlopts.OptRaceMode.ValueFrom(opts)
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
		for i := int64(0); i < parallelism; i++ {
			cli := getHttpCli(proxyReq.Host, opts, false)
			ctx, cancel := context.WithCancel(proxyReq.Context())
			req := proxyReq.WithContext(ctx)
			cancels = append(cancels, cancel)
			go func(i int) {
				logger.Debugf("[RACE] doing concurrent request, idx: %d", i)
				resp, err := doRequestSerial(cli, req, opts)
				ch <- result{resp, err, i}
			}(int(i))
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
				logger.Errorf("[RACE] request context done, url: %s, err: %s",
					proxyReq.URL.String(), proxyReq.Context().Err())
				lastErr = proxyReq.Context().Err()
				count = 0 // break for loop
			case r := <-ch:
				lastResp = r.resp
				lastErr = r.err
				lastIdx = r.idx
				if r.err == nil && goodStatusCode(r.resp.StatusCode) {
					logger.Debugf("[RACE] got final response")
					return r.resp, r.err
				}
				if logger.IsDebug() {
					statusCode := 0
					if r.resp != nil {
						statusCode = r.resp.StatusCode
					}
					logger.Debugf("[RACE] got bad response, status code: %d, err: %v",
						statusCode, r.err)
				}
				count--
			}
		}
		return lastResp, lastErr
	} else {
		cli := getHttpCli(proxyReq.Host, opts, true)
		return doRequestSerial(cli, proxyReq, opts)
	}
}

func doRequestSerial(cli *http.Client, proxyReq *http.Request, opts *urlopts.Options) (*http.Response, error) {
	retriesNon2xx, _ := urlopts.OptRetriesNon2xx.ValueFrom(opts)
	retriesError, _ := urlopts.OptRetriesError.ValueFrom(opts)

	const maxRetryDelay = time.Second
	retryDelay := 100 * time.Millisecond
	raiseRetryDelay := func() {
		retryDelay = 2 * retryDelay
		if retryDelay > maxRetryDelay {
			retryDelay = maxRetryDelay
		}
	}

	for {
		if antiCaching, _ := urlopts.OptAntiCaching.ValueFrom(opts); antiCaching {
			query := proxyReq.URL.Query()
			query.Set("__t", strconv.FormatInt(time.Now().UnixNano(), 10))
			proxyReq.URL.RawQuery = query.Encode()
		}

		resp, err := cli.Do(proxyReq)
		if err != nil {
			logger.Errorf("do request failed, url: %s, err: %s", proxyReq.URL.String(), err)
			if retriesError == 0 {
				return nil, err
			}
			logger.Debugf("url: %s, err: %s. retry for errors, remaining retries: %d",
				proxyReq.URL.String(), err, retriesError)
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
			logger.Debugf("url: %s, status code: %d. retry for non-2xx, remaining retries: %d",
				proxyReq.URL.String(), resp.StatusCode, retriesNon2xx)
			if _, err := io.Copy(io.Discard, resp.Body); err != nil {
				logger.Errorf("discard response body failed, err: %s", err)
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

func ForwardResponse(w http.ResponseWriter, proxyResp *http.Response) {
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
	opts := urlopts.Options{}
	dialCtxFn, _ := getDialer(req.Host, &opts)
	conn, err := dialCtxFn(req.Context(), "tcp", req.URL.Host)
	if err != nil {
		logger.Errorf("dial to %s failed, err: %s", req.URL.Host, err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(err.Error()))
		return
	}
	defer conn.Close()
	inConn, bufrw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		logger.Errorf("hijack failed, err: %s", err)
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
		// overcome by using its underlying net.TCPConn.
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

func rewriteLocation(resp *http.Response, req *http.Request, opts *urlopts.Options) {
	if req.URL.Scheme != "" {
		// don't rewrite location for regular proxy requests, it should
		// be the responsibility of the client.
		return
	}
	loc, err := resp.Location()
	if err != nil {
		return
	}
	logger.Debugf("location: %s", resp.Header["Location"])
	if rewrite, _ := urlopts.OptRewriteRedirect.ValueFrom(opts); rewrite {
		loc = urlopts.RelocateToUrlproxy(loc, opts)
		resp.Header.Set("Location", loc.String())
	}
}

func Handle(w http.ResponseWriter, req *http.Request, opts *urlopts.Options) bool {
	if req.Method == http.MethodConnect {
		handleConnectMethod(w, req)
		return true
	}

	proxyReq, err := prepareProxyRequest(req, opts)
	if err != nil {
		logger.Errorf("prepare proxy request failed, err: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return true
	}

	if timeoutMs, _ := urlopts.OptTimeoutMs.ValueFrom(opts); timeoutMs > 0 {
		var ctx context.Context
		ctx, cancel := context.WithTimeout(req.Context(), time.Duration(timeoutMs)*time.Millisecond)
		proxyReq = proxyReq.WithContext(ctx)
		defer cancel()
	}

	proxyResp, err := doRequest(proxyReq, opts)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(err.Error()))
		return true
	}
	rewriteLocation(proxyResp, req, opts)
	ForwardResponse(w, proxyResp)
	return true
}
