package http

import "net/http"

type ResponseWriterWrapper struct {
	http.ResponseWriter
}

func (rw ResponseWriterWrapper) Header() HeaderWrapper {
	return HeaderWrapper{rw.ResponseWriter.Header()}
}

func (rw ResponseWriterWrapper) WriteHeader(statusCode int) ResponseWriterWrapper {
	rw.ResponseWriter.WriteHeader(statusCode)
	return rw
}
