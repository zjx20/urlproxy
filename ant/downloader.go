package ant

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/zjx20/urlproxy/logger"
)

var (
	wholeFile = dataRange{
		begin: -1,
		end:   -1,
	}

	errAlreadyStarted = fmt.Errorf("already started")
	errStopped        = fmt.Errorf("stopped")
	errNotDownloaded  = fmt.Errorf("not downloaded")
	errBadStatus      = fmt.Errorf("bad status")
	errDestroyed      = fmt.Errorf("destroyed")

	maxAnts = 10
)

type probeResultEv struct {
	total int64
	err   error
}

type downloadDoneEv struct {
	reqRange  dataRange
	probing   bool
	err       error
	temporary bool
}

type waiter struct {
	offset int64
	ch     chan struct{}
}

type RequestManipulator func(req *http.Request)

type Downloader struct {
	mu        sync.Mutex
	pieceSize int
	ants      int
	url       string
	save      string
	rm        RequestManipulator
	timeout   time.Duration
	f         *os.File

	cancelCtx context.Context
	cancel    context.CancelFunc
	stopped   bool

	startTime         time.Time
	status            Status
	totalSize         int64
	downloaded        *space
	dataWaiters       []waiter
	completionWaiters []chan struct{}
}

func NewDownloader(pieceSize int, ants int, url string, save string,
	rm RequestManipulator, timeout time.Duration) *Downloader {
	if pieceSize <= 0 {
		panic(fmt.Sprintf("pieceSize %d is invalid", pieceSize))
	}
	if ants > maxAnts {
		ants = maxAnts
	}
	d := &Downloader{
		pieceSize: pieceSize,
		ants:      ants,
		url:       url,
		save:      save,
		rm:        rm,
		timeout:   timeout,
	}
	d.init()
	return d
}

func (d *Downloader) init() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancelCtx = ctx
	d.cancel = cancel
	d.status = NotStarted
	d.totalSize = -1
	d.downloaded = newSpace()
}

func (d *Downloader) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status != NotStarted {
		return errAlreadyStarted
	}
	d.startTime = time.Now()
	d.status = Started
	dir := path.Dir(d.save)
	if err := os.MkdirAll(dir, 0755); err != nil {
		d.finishLocked(fmt.Errorf("os.MkdirAll(%s) failed: %v", dir, err))
		return err
	}
	f, err := os.OpenFile(d.save, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		d.finishLocked(fmt.Errorf("os.OpenFile(%s) failed: %v", d.save, err))
		return err
	}
	d.f = f
	go d.brain(d.cancelCtx)
	logger.Debugf("[ant] start %s", d.url)
	return nil
}

func (d *Downloader) Retry() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !(d.status == NotStarted || d.status == Aborted) {
		return errBadStatus
	}
	d.stopped = false
	if d.f != nil {
		d.f.Close()
		os.Remove(d.save)
		d.f = nil
	}
	d.init()
	return nil
}

func (d *Downloader) Destroy() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.finishLocked(errDestroyed)
	d.f.Close()
	os.Remove(d.save)
	d.status = Destroyed
}

func (d *Downloader) Status() (status Status, total int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status, d.totalSize
}

func (d *Downloader) notifyWaitReadyLocked(hint dataRange) {
	if hint == wholeFile {
		for _, w := range d.dataWaiters {
			close(w.ch)
		}
		d.dataWaiters = nil
	} else {
		pos := 0
		for idx := range d.dataWaiters {
			offset := d.dataWaiters[idx].offset
			if hint.begin <= offset && offset < hint.end {
				close(d.dataWaiters[idx].ch)
			} else {
				if idx > pos {
					d.dataWaiters[pos] = d.dataWaiters[idx]
				}
				pos++
			}
		}
		d.dataWaiters = d.dataWaiters[:pos]
	}
}

func (d *Downloader) notifyCompletionLocked() {
	for _, ch := range d.completionWaiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	d.completionWaiters = nil
}

func (d *Downloader) finish(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.finishLocked(err)
}

func (d *Downloader) finishLocked(err error) {
	if !d.stopped {
		d.cancel()
		d.stopped = true
		d.notifyWaitReadyLocked(wholeFile)
		d.notifyCompletionLocked()
	}
	switch d.status {
	case NotStarted, Started, Downloading:
		if err != nil {
			if err != errDestroyed {
				logger.Errorf("[ant] %s aborted with error: %s, spent: %s",
					d.url, err, time.Since(d.startTime))
			}
			d.status = Aborted
		} else {
			d.status = Completed
			if d.totalSize < 0 {
				total := d.downloaded.coveredRange(0)
				d.totalSize = total.end
			}
			logger.Infof("[ant] %s download completed, size %d, spent %s",
				d.url, d.totalSize, time.Since(d.startTime))
		}
	}
}

func (d *Downloader) brain(ctx context.Context) {
	runnings := 0
	tolerance := 0
	multiThreads := false
	downloadingWhole := false
	totalSize := int64(0)
	progress := int64(0)
	eventCh := make(chan interface{}, 16)
	var lastFailedRange dataRange

	if d.ants > 1 {
		// issue the probing request
		runnings++
		go d.download(ctx, eventCh, true, dataRange{
			begin: 0,
			end:   int64(d.pieceSize),
		})
	} else {
		// drive the loop manually
		eventCh <- &probeResultEv{
			err: fmt.Errorf("disabled multi-threads downloading"),
		}
	}

	for {
		select {
		case obj := <-eventCh:
			// probeResultEv is guaranteed to be appeared before downloadDoneEv
			switch ev := obj.(type) {
			case *probeResultEv:
				logger.Debugf("[ant] url: %s probeResultEv: %+v, err: %v",
					d.url, ev, ev.err)
				if ev.err == nil {
					multiThreads = true
					tolerance = 3
					totalSize = ev.total
					progress = int64(d.pieceSize)
				}
				d.mu.Lock()
				d.status = Downloading
				d.mu.Unlock()
			case *downloadDoneEv:
				logger.Debugf("[ant] url: %s downloadDoneEv: %+v, err: %v",
					d.url, ev, ev.err)
				runnings--
				if ev.err != nil {
					if ev.probing {
						break
					}
					if !ev.temporary {
						d.finish(fmt.Errorf("download %s failed: %v", d.url, ev.err))
						return
					} else {
						tolerance--
						if tolerance < 0 {
							d.finish(fmt.Errorf("download %s failed: %v", d.url, ev.err))
							return
						}
						// TODO: skip downloaded range
						lastFailedRange = ev.reqRange
						break
					}
				}
			default:
				logger.Errorf("unknown event: %+v", obj)
			}
		case <-ctx.Done():
			return
		}

		// reconcile
		if multiThreads {
			for runnings < d.ants {
				var reqRange dataRange
				if lastFailedRange != zeroRange {
					reqRange = lastFailedRange
					lastFailedRange = zeroRange
				} else if progress < totalSize {
					reqRange = dataRange{
						begin: progress,
						end:   min(progress+int64(d.pieceSize), totalSize),
					}
					progress += int64(d.pieceSize)
				}
				if reqRange != zeroRange {
					runnings++
					go d.download(ctx, eventCh, false, reqRange)
				} else {
					break
				}
			}
		} else {
			if !downloadingWhole {
				runnings++
				go d.download(ctx, eventCh, false, wholeFile)
				downloadingWhole = true
			}
		}
		if runnings == 0 {
			logger.Infof("[ant] download %s finished", d.url)
			d.finish(nil)
			return
		}
	}
}

func (d *Downloader) writeAt(data []byte, offset int64) error {
	_, err := d.f.WriteAt(data, offset)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	newRange := dataRange{offset, offset + int64(len(data))}
	d.downloaded.cover(newRange)
	// notify waiters
	d.notifyWaitReadyLocked(newRange)
	return nil
}

func (d *Downloader) download(ctx context.Context,
	eventCh chan interface{}, probing bool, r dataRange) {
	sendEv := func(ev interface{}) {
		select {
		case eventCh <- ev:
		case <-ctx.Done():
		}
	}
	feedback := func(err error, temporary bool) {
		if probing && err != nil {
			// must report probeResultEv before downloadDoneEv
			sendEv(&probeResultEv{
				err: err,
			})
		}
		sendEv(&downloadDoneEv{
			reqRange:  r,
			probing:   probing,
			err:       err,
			temporary: temporary,
		})
	}
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
	if err != nil {
		logger.Errorf("new request error: %s", err)
		feedback(err, false)
		return
	}
	if r != wholeFile {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.begin, r.end-1))
	}
	if d.rm != nil {
		d.rm(req)
	}
	dog := newWatchDog(cancelFn, d.timeout)
	defer dog.stop()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Errorf("do request error: %s", err)
		feedback(err, true)
		return
	}

	// Cases that the server doesn't support ranged http request:
	//   * code 206, but doesn't have a valid Content-Range header
	//   * code 400
	//
	// If the range of the request is partly beyond the file size,
	// the server will still return the correct range.
	// If the scope of the request completely exceeds the file size,
	// the server will return code 416.

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		err := fmt.Errorf("bad http code: %d", resp.StatusCode)
		temporary := false
		// we may get 404 if the file has not been pushed to the CDN
		if resp.StatusCode == 404 {
			temporary = true
		}
		feedback(err, temporary)
		return
	}
	offset := int64(0)
	if r != wholeFile {
		crange := resp.Header.Get("Content-Range")
		total, respRange, err := parseContentRange(crange)
		if err != nil {
			err = fmt.Errorf("cannot parse Content-Range, err: %s", err)
			feedback(err, false)
			return
		}
		if r.begin != respRange.begin {
			err = fmt.Errorf("response range (%+v) doesn't match the request (%+v)",
				respRange, r)
			feedback(err, false)
			return
		}
		offset = respRange.begin
		if probing {
			d.mu.Lock()
			d.totalSize = total
			d.mu.Unlock()
			sendEv(&probeResultEv{
				total: total,
			})
		}
	} else {
		if resp.ContentLength != -1 {
			d.mu.Lock()
			d.totalSize = resp.ContentLength
			d.mu.Unlock()
		}
	}

	buf := make([]byte, 32*1024)
	for {
		dog.feed()
		n, err := resp.Body.Read(buf)
		if n > 0 {
			werr := d.writeAt(buf[:n], offset)
			if werr != nil {
				d.finish(fmt.Errorf("write to file err: %s, offset: %d",
					werr, offset))
				return
			}
			offset += int64(n)
		}
		if err == io.EOF {
			break
		} else if err != nil {
			logger.Errorf("read response body err: %s", err)
			feedback(err, true)
			return
		}
	}

	feedback(nil, false)
}

func (d *Downloader) checkOffsetLocked(offset int64) error {
	if offset < 0 {
		return fmt.Errorf("invalid offset %d", offset)
	}
	if d.downloaded.isCovered(offset) {
		return nil
	}
	if d.totalSize != -1 {
		if offset >= d.totalSize {
			return io.EOF
		}
	}
	if d.stopped {
		return errStopped
	}
	return errNotDownloaded
}

func (d *Downloader) WaitReady(ctx context.Context, offset int64) error {
	d.mu.Lock()
	err := d.checkOffsetLocked(offset)
	if err == nil || err != errNotDownloaded {
		// err == nil means the offset is already downloaded, so we don't need to wait.
		// err != errNotDownloaded means some error happened.
		d.mu.Unlock()
		return err
	}
	ch := make(chan struct{})
	d.dataWaiters = append(d.dataWaiters, waiter{
		offset: offset,
		ch:     ch,
	})
	d.mu.Unlock()

	select {
	case <-ch:
		// check again
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.checkOffsetLocked(offset)
	case <-ctx.Done():
		d.mu.Lock()
		defer d.mu.Unlock()
		pos := -1
		for idx := range d.dataWaiters {
			if d.dataWaiters[idx].ch == ch {
				pos = idx
				break
			}
		}
		if pos != -1 {
			copy(d.dataWaiters[pos:], d.dataWaiters[pos+1:])
			d.dataWaiters = d.dataWaiters[:len(d.dataWaiters)-1]
			close(ch)
		}
		return ctx.Err()
	}
}

func (d *Downloader) ReadAt(ctx context.Context, b []byte, off int64) (int, error) {
	err := d.WaitReady(ctx, off)
	if err != nil {
		return 0, err
	}
	d.mu.Lock()
	r := d.downloaded.coveredRange(off)
	n := int(r.end - r.begin)
	if n > len(b) {
		n = len(b)
	}
	d.mu.Unlock()
	return d.f.ReadAt(b[:n], off)
}

func (d *Downloader) AddCompletionListener(ch chan struct{}) {
	if cap(ch) == 0 {
		panic("must be a buffered channel")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		select {
		case ch <- struct{}{}:
		default:
		}
		return
	}
	d.completionWaiters = append(d.completionWaiters, ch)
}

var (
	contentRangeRegexp = regexp.MustCompile(`bytes\s+(\d+)-(\d)+/(\d+)`)
)

func parseContentRange(x string) (total int64, r dataRange, err error) {
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Range
	//
	// Valid Syntax
	//   Content-Range: <unit> <range-start>-<range-end>/<size>
	//   Content-Range: <unit> <range-start>-<range-end>/*
	//   Content-Range: <unit> */<size>
	//
	// Only need the first one for our case.

	match := contentRangeRegexp.FindStringSubmatch(x)
	if len(match) != 4 {
		err = fmt.Errorf("invalid Content-Range value")
		return
	}
	if r.begin, err = strconv.ParseInt(match[1], 10, 64); err != nil {
		return
	}
	if r.end, err = strconv.ParseInt(match[2], 10, 64); err != nil {
		return
	}
	if total, err = strconv.ParseInt(match[3], 10, 64); err != nil {
		return
	}
	return
}
