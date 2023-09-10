package tpl

import (
	"bufio"
	"net/http"
	"text/template"

	sprig "github.com/go-task/slim-sprig"

	tplhttp "github.com/zjx20/urlproxy/tpl/http"
)

type renderContext struct {
	Request        *http.Request
	ResponseWriter tplhttp.ResponseWriterWrapper
	ExtraValues    map[string]interface{}
}

func render(tpl string, rw http.ResponseWriter, req *http.Request,
	extraValues map[string]interface{}) error {
	fmap := sprig.TxtFuncMap()
	t, err := template.New("").Funcs(fmap).Funcs(tplhttp.Funcs()).Parse(tpl)
	if err != nil {
		return err
	}
	rCtx := renderContext{
		Request: req,
		ResponseWriter: tplhttp.ResponseWriterWrapper{
			ResponseWriter: rw,
		},
		ExtraValues: extraValues,
	}
	// Wrapping bufio here is not only for buffering, but also for delaying
	// the call to rw.Write(), which can avoid sending the 200 status code
	// too early. When rendering fails, there is a chance to return the 500
	// status code to the client.
	bw := bufio.NewWriterSize(rw, 8192)
	defer bw.Flush()
	return t.Execute(bw, rCtx)
}
