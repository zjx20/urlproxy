package tpl

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"text/template"

	sprig "github.com/go-task/slim-sprig"
	"github.com/zjx20/urlproxy/logger"
	tplhttp "github.com/zjx20/urlproxy/tpl/http"
	"github.com/zjx20/urlproxy/tpl/storage"
)

type renderContext struct {
	Request        *http.Request
	ResponseWriter tplhttp.ResponseWriterWrapper
	ExtraValues    map[string]interface{}
}

func tplFuncs(tPtr **template.Template) template.FuncMap {
	return template.FuncMap{
		"execTemplate": func(name string, data interface{}) (string, error) {
			buf := &bytes.Buffer{}
			err := (*tPtr).ExecuteTemplate(buf, name, data)
			return buf.String(), err
		},
		"debug": func(format string, args ...interface{}) string {
			msg := fmt.Sprintf(format, args...)
			logger.Debugf("%s", msg)
			return msg
		},
	}
}

func render(tpl string, rw http.ResponseWriter, req *http.Request,
	extraValues map[string]interface{}) error {
	var t *template.Template
	t, err := template.New("").
		Funcs(tplFuncs(&t)).
		Funcs(sprig.TxtFuncMap()).
		Funcs(tplhttp.Funcs()).
		Funcs(storage.Funcs()).
		Parse(tpl)
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
	err = t.Execute(bw, rCtx)
	if err != nil {
		logger.Errorf("tpl render error: %s", err)
		rw.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(bw, "Error: %s", err)
	}
	return err
}
