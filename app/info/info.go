package info

import "net"

var (
	listenAddr net.Addr
)

func GetListenAddr() net.Addr {
	return listenAddr
}

func SetListenAddr(addr net.Addr) {
	listenAddr = addr
}
