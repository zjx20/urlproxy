package handler

import (
	"net/http"

	"github.com/zjx20/urlproxy/urlopts"
)

type (
	HttpHandler func(http.ResponseWriter, *http.Request, *urlopts.Options) bool
)

var (
	stack []HttpHandler
)

func defaultHandler(w http.ResponseWriter, r *http.Request, opts *urlopts.Options) bool {
	w.WriteHeader(500)
	w.Write([]byte("not implemented"))
	return true
}

func Register(handler HttpHandler) {
	stack = append(stack, handler)
}

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	after, opts := urlopts.Extract(r.URL)
	r.URL = &after
	for _, h := range stack {
		ok := h(w, r, opts)
		if ok {
			return
		}
	}
	defaultHandler(w, r, opts)
}
