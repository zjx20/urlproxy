package http

import (
	"io"
	"net/http"
)

type ResponseWrapper struct {
	*http.Response
}

func (resp ResponseWrapper) Text() (string, error) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	return string(data), nil
}
