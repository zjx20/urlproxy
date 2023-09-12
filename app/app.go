package app

import (
	"flag"
	"net"
	"net/http"

	"github.com/zjx20/urlproxy/app/info"
	"github.com/zjx20/urlproxy/handler"
	"github.com/zjx20/urlproxy/hlsboost"
	"github.com/zjx20/urlproxy/kvstore"
	"github.com/zjx20/urlproxy/logger"
	"github.com/zjx20/urlproxy/proxy"
)

var (
	bind = flag.String("bind", "0.0.0.0:8765", "Address to bind")

	kvstoreDir       = flag.String("kvstore-dir", "./kvdata", "Directory of kvstore")
	kvstoreCacheSize = flag.Uint("kvstore-cache-size", 128*1024, "Size of in-memory cache of kvstore")
)

func Run() {
	flag.Parse()
	if *kvstoreDir != "" {
		err := kvstore.InitKVStore(*kvstoreDir, *kvstoreCacheSize)
		if err != nil {
			logger.Warnf("kvstore is unavailable because init failed, err: %v", err)
		} else {
			logger.Infof("kvstore is ready")
		}
	}
	ln, err := net.Listen("tcp", *bind)
	if err != nil {
		logger.Fatalf("listen to %s failed, err: %v", *bind, err)
		return
	}
	logger.Infof("listen to %s", ln.Addr().String())
	info.SetListenAddr(ln.Addr())
	selfCli := hlsboost.NewSelfClient("http", ln.Addr().String())
	handler.Register(hlsboost.Handler(selfCli)) // TODO: refactor
	handler.Register(proxy.Handle)
	http.Serve(ln, http.HandlerFunc(handler.ServeHTTP))
	logger.Infof("exit")
}
