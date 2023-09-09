package proxy

import (
	"io"
	"net/http"
	"os/exec"

	"github.com/zjx20/urlproxy/logger"
)

type pipeOut struct {
	wrote bool
	out   io.Writer
}

func (o *pipeOut) Write(buf []byte) (int, error) {
	if len(buf) > 0 {
		o.wrote = true
		return o.out.Write(buf)
	}
	return 0, nil
}

type errLogger struct {
	cmd         string
	loggedFirst bool
}

func (l *errLogger) Write(buf []byte) (int, error) {
	if !l.loggedFirst {
		l.loggedFirst = false
		logger.Errorf("[pipe] cmd %s stderr:", l.cmd)
	}
	logger.Errorf("%s", string(buf))
	return len(buf), nil
}

func pipe(w http.ResponseWriter, req *http.Request, proxyResp *http.Response, cmd string) {
	logger.Debugf("[pipe] begin exec cmd %s, req: %s", cmd, req.URL.String())
	c := exec.CommandContext(req.Context(), "/bin/sh", "-c", cmd)
	c.Stdin = proxyResp.Body
	out := &pipeOut{out: w}
	c.Stdout = out
	c.Stderr = &errLogger{cmd: cmd}
	if err := c.Run(); err != nil {
		logger.Errorf("[pipe] end exec cmd %s failed, req: %s, err: %s",
			cmd, req.URL.String(), err)
		if !out.wrote {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
		}
		return
	}
	proxyResp.Body.Close()
	logger.Debugf("[pipe] end exec cmd %s success, req: %s", cmd, req.URL.String())
}
