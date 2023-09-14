package http

import (
	"fmt"
	"io"
	"net/http"
)

type ResponseWrapper struct {
	*http.Response
	err error
}

func (resp ResponseWrapper) Error() error {
	return resp.err
}

func (resp ResponseWrapper) Text() (string, error) {
	if resp.Response == nil {
		return "", fmt.Errorf("response is nil")
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	return string(data), nil
}

func (resp ResponseWrapper) LastLocation() string {
	if resp.Response == nil {
		return ""
	}
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return ""
}
