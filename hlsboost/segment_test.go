package hlsboost

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
	"testing"

	"github.com/zjx20/urlproxy/ant"
	"github.com/zjx20/urlproxy/urlopts"
)

func prepareHttpServer(content []byte) *httptest.Server {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write(content)
	}
	return httptest.NewServer(http.HandlerFunc(handler))
}

func TestSegmentPrefetch(t *testing.T) {
	randContent := make([]byte, 128*1024)
	_, err := rand.Read(randContent)
	if err != nil {
		t.Fatal(err)
	}
	server := prepareHttpServer(randContent)
	defer server.Close()

	selfCli := NewSelfClient("http", server.Listener.Addr().String())
	url := selfCli.ToFinalUrl("/", "/test.data", &urlopts.Options{})
	seg := newSegment(10, "testseg", url, "./testdata", &urlopts.Options{})

	// prefetch returns true for the first time
	ok := seg.Prefetch()
	if !ok {
		t.Fatal("must ok")
	}
	ok = seg.Prefetch()
	if ok {
		t.Fatal("must not ok")
	}

	notifyCh := make(chan struct{}, 1)
	seg.AddCompletionListener(notifyCh)

	// get total size
	total, err := seg.TotalSize(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if total != int64(len(randContent)) {
		t.Fatalf("total should be %d, got %d", len(randContent), total)
	}

	// read cached data
	var data []byte
	buf := make([]byte, 4*1024)
	off := int64(0)
	seg.Acquire()
	for {
		n, err := seg.ReadAt(context.Background(), buf, off)
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
	default:
		t.Fatalf("should notify")
	}

	// should notify immediately
	notifyCh2 := make(chan struct{}, 1)
	seg.AddCompletionListener(notifyCh2)
	select {
	case <-notifyCh2:
	default:
		t.Fatalf("should notify")
	}

	// check status when prefetch done
	status := seg.Status()
	if !ant.IsCompleted(status) {
		t.Fatalf("should be completed, got %d", status)
	}

	// destroy should fail
	err = seg.Destroy(false)
	if err == nil {
		t.Fatal("should error")
	}

	// lazy destroy
	err = seg.Destroy(true)
	if err != nil {
		t.Fatal("should error")
	}

	// got destroyed if no one is referencing it
	seg.Release()

	// check status when destroyed
	status = seg.Status()
	if status != ant.Destroyed {
		t.Fatalf("should be destroyed, got %d", status)
	}

	_, err = os.Stat(fmt.Sprintf("testdata/%s", seg.segId))
	if !os.IsNotExist(err) {
		t.Fatalf("should be 'not exist' error, got %s", err)
	}
}
