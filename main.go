package main

import (
	"flag"
	"net"
	"net/http"

	"github.com/zjx20/urlproxy/logger"
	"github.com/zjx20/urlproxy/proxy"
)

var (
	bind = flag.String("bind", "0.0.0.0:8765", "Address to bind")
)

func main() {
	flag.Parse()
	ln, err := net.Listen("tcp", *bind)
	if err != nil {
		logger.Fatalf("listen to %s failed, err: %v", *bind, err)
		return
	}
	logger.Infof("listen to %s", ln.Addr().String())
	http.Serve(ln, http.HandlerFunc(proxy.ProxyHandler))
	logger.Infof("exit")
}
