package http

import "net/http"

type HeaderWrapper struct {
	http.Header
}

func (h HeaderWrapper) Add(key, value string) HeaderWrapper {
	h.Header.Add(key, value)
	return h
}

func (h HeaderWrapper) Set(key, value string) HeaderWrapper {
	h.Header.Set(key, value)
	return h
}
