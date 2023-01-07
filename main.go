package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
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
)

const (
	optPrefix = "urlproxyOpt"
	optHeader = optPrefix + "Header"
	optSchema = optPrefix + "Schema"
	optSocks  = optPrefix + "Socks"
	optDns    = optPrefix + "Dns"
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
	instUUID = uuid.NewString()
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

func getDialer(opts url.Values) (dialer, bool) {
	var direct dialer = &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if opts.Has(optDns) {
		dns := opts.Get(optDns)
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
			return direct, false
		}
	}
	pd := proxy.FromEnvironmentUsing(direct)
	if socksAddr != "" {
		pd, _ = proxy.SOCKS5("tcp", socksAddr, nil, direct)
	} else if *socksUds != "" {
		pd, _ = proxy.SOCKS5("unix", *socksUds, nil, direct)
	}
	if pd == nil {
		return direct, false
	} else {
		return pd.(dialer), pd.(dialer) != direct
	}
}

func getHttpCli(opts url.Values) *http.Client {
	d, socksed := getDialer(opts)
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
	return &http.Client{Transport: transport}
}

func forward(from, to *connEx, wg *sync.WaitGroup) {
	io.Copy(to, from)
	from.CloseRead()
	to.CloseWrite()
	wg.Done()
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

	proxyReq, err = http.NewRequestWithContext(
		req.Context(), req.Method, proxyUrl.String(), req.Body)
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
	for _, header := range opts[optHeader] {
		if header == "" {
			continue
		}
		parts := append(strings.SplitN(header, ":", 2), "")
		proxyReq.Header.Set(parts[0], strings.TrimLeftFunc(parts[1], unicode.IsSpace))
	}

	return
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
	d, _ := getDialer(opts)
	conn, err := d.DialContext(req.Context(), "tcp", req.URL.Host)
	if err != nil {
		log.Printf("ERR: dial to %s failed, err: %s", req.URL.Host, err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(err.Error()))
		return
	}
	defer conn.Close()
	inConn, bufrw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Printf("ERR: hijack failed, err: %s", err)
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
		log.Printf("ERR: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	proxyResp, err := getHttpCli(opts).Do(proxyReq)
	if err != nil {
		log.Printf("ERR: %s", err)
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
		log.Fatalf("listen to %s failed, err: %v", *bind, err)
		return
	}
	log.Printf("Listen to %s", ln.Addr().String())
	http.Serve(ln, http.HandlerFunc(httpProxyHandler))
	log.Printf("exit")
}
