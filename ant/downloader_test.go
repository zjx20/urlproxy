package ant

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zjx20/urlproxy/logger"
)

func init() {
	logger.SetDebug(true)
}

type slowContent struct {
	bps     int
	content []byte
	off     int
}

var _ io.ReadSeeker = (*slowContent)(nil)

func (c *slowContent) Read(p []byte) (n int, err error) {
	if c.off >= len(c.content) {
		return 0, io.EOF
	}
	time.Sleep(1 * time.Second)
	n = c.bps
	if n > len(p) {
		n = len(p)
	}
	copy(p, c.content[c.off:c.off+n])
	c.off += n
	return n, nil
}

func (c *slowContent) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		c.off = int(offset)
		return int64(c.off), nil
	case io.SeekCurrent:
		c.off += int(offset)
		return int64(c.off), nil
	case io.SeekEnd:
		c.off = len(c.content) + int(offset)
		return int64(c.off), nil
	default:
		return 0, fmt.Errorf("invalid whence")
	}
}

func prepareSlowHttpServer(bps int, content []byte) *httptest.Server {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "support-partial-content") {
			c := &slowContent{
				bps:     bps,
				content: content,
			}
			http.ServeContent(w, r, "", time.Now(), c)
		} else {
			if r.Header.Get("Range") != "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			withContentLength := true
			if strings.Contains(r.URL.Path, "no-content-length") {
				withContentLength = false
			}
			if withContentLength {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			}
			rest := len(content)
			offset := 0
			for rest > 0 {
				n := bps
				if n > rest {
					n = rest
				}
				w.Write(content[offset : offset+n])
				offset += n
				rest -= n
				time.Sleep(1 * time.Second)
			}
		}
	}
	return httptest.NewServer(http.HandlerFunc(handler))
}

func TestDownloader(t *testing.T) {
	randContent := make([]byte, 96*1024)
	_, err := rand.Read(randContent)
	if err != nil {
		t.Fatal(err)
	}
	server := prepareSlowHttpServer(24*1024, randContent)
	defer server.Close()

	base := fmt.Sprintf("http://%s", server.Listener.Addr().String())

	const (
		defaultPieceSize = 32 * 1024
		defaultAnts      = 5
	)
	cases := []struct {
		name              string
		url               string
		knowContentLength bool
		pieceSize         int
		ants              int
	}{
		{"multi thread downloading", base + "/support-partial-content", true, defaultPieceSize, defaultAnts},
		{"single thread downloading with content length", base + "/foo", true, defaultPieceSize, defaultAnts},
		{"single thread downloading without content length", base + "/no-content-length", false, defaultPieceSize, defaultAnts},
		{"disable multi thread downloading", base + "/support-partial-content", true, defaultPieceSize, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testDownloader(t, c.pieceSize, c.ants, c.url,
				randContent, c.knowContentLength)
		})
	}
}

func testDownloader(t *testing.T, pieceSize int, ants int,
	url string, randContent []byte, knowContentLength bool) {
	file := "./testdata/file"
	d := NewDownloader(pieceSize, ants, url, file, nil)

	// check status
	status, total := d.Status()
	if status != NotStarted {
		t.Fatalf("should be not started, got %d", status)
	}
	if total != -1 {
		t.Fatalf("total should be -1, got %d", total)
	}

	err := d.Start()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// check status when download started
	err = d.WaitReady(context.Background(), 0)
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	status, total = d.Status()
	if !IsStarted(status) {
		t.Fatalf("should be started, got %d", status)
	}
	if knowContentLength {
		if total != int64(len(randContent)) {
			t.Fatalf("total should be %d, got %d", len(randContent), total)
		}
	} else {
		if total != -1 {
			t.Fatalf("total should be -1, got %d", total)
		}
	}

	notifyCh := make(chan struct{}, 1)
	d.AddCompletionListener(notifyCh)

	// read cached data
	var data []byte
	buf := make([]byte, 4*1024)
	off := int64(0)
	for {
		n, err := d.ReadAt(context.Background(), buf, off)
		t.Logf("read %d bytes, offset: %d", n, off)
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatalf("err: %s", err)
		}
		if !reflect.DeepEqual(randContent[off:off+int64(n)], buf[:n]) {
			t.Logf("randContent[%d:%d]: %q", off, off+int64(n), randContent[off:off+int64(n)])
			t.Logf("               buf: %q", buf[:n])
			t.FailNow()
		}
		data = append(data, buf[:n]...)
		off += int64(n)
	}

	if !reflect.DeepEqual(randContent, data) {
		t.Logf("randContent: %q", randContent)
		t.Logf("       data: %q", data)
		t.FailNow()
	}

	select {
	case <-notifyCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("should notify")
	}

	// should notify immediately
	notifyCh2 := make(chan struct{}, 1)
	d.AddCompletionListener(notifyCh2)
	select {
	case <-notifyCh2:
	default:
		t.Fatalf("should notify")
	}

	// check status when download completed
	status, total = d.Status()
	if !IsCompleted(status) {
		t.Fatalf("should be completed, got %d", status)
	}
	if !knowContentLength {
		// get final size when download completed
		if total != int64(len(randContent)) {
			t.Fatalf("total should be %d, got %d", len(randContent), total)
		}
	}

	d.Destroy()

	// check status when destroyed
	status, _ = d.Status()
	if status != Destroyed {
		t.Fatalf("should be destroyed, got %d", status)
	}

	_, err = os.Stat(file)
	if !os.IsNotExist(err) {
		t.Fatalf("should be 'not exist' error, got %s", err)
	}
}
