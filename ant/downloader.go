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

type Downloader struct {
	mu        sync.Mutex
	pieceSize int
	ants      int
	url       string
	save      string
	f         *os.File

	eventCh   chan interface{}
	cancelCtx context.Context
	cancel    context.CancelFunc
	stopped   bool

	status            Status
	totalSize         int64
	downloaded        *space
	dataWaiters       []waiter
	completionWaiters []chan struct{}
}

func NewDownloader(pieceSize int, ants int, url string, save string) *Downloader {
	if pieceSize <= 0 {
		panic(fmt.Sprintf("pieceSize %d is invalid", pieceSize))
	}
	if ants > maxAnts {
		ants = maxAnts
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Downloader{
		pieceSize:  pieceSize,
		ants:       ants,
		url:        url,
		save:       save,
		eventCh:    make(chan interface{}, 16),
		cancelCtx:  ctx,
		cancel:     cancel,
		status:     NotStarted,
		totalSize:  -1,
		downloaded: newSpace(),
	}
}

func (d *Downloader) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status != NotStarted {
		return errAlreadyStarted
	}
	d.status = Started
	dir := path.Dir(d.save)
	if err := os.MkdirAll(dir, 0755); err != nil {
		d.finish(true)
		return err
	}
	f, err := os.OpenFile(d.save, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		d.finish(true)
		return err
	}
	d.f = f
	go d.brain()
	return nil
}

func (d *Downloader) Destroy() {
	d.finish(true)
	d.mu.Lock()
	defer d.mu.Unlock()
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

func (d *Downloader) finish(abort bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.stopped {
		d.cancel()
		d.stopped = true
		d.notifyWaitReadyLocked(wholeFile)
		d.notifyCompletionLocked()
	}
	switch d.status {
	case NotStarted, Started, Downloading:
		if abort {
			d.status = Aborted
		} else {
			d.status = Completed
			if d.totalSize < 0 {
				total := d.downloaded.coveredRange(0)
				d.totalSize = total.end
			}
		}
	}
}

func (d *Downloader) brain() {
	runnings := 0
	tolerance := 0
	multiThreads := false
	downloadingWhole := false
	totalSize := int64(0)
	progress := int64(0)
	var lastFailedRange dataRange

	if d.ants > 1 {
		// issue the probing request
		runnings++
		go d.download(true, dataRange{
			begin: 0,
			end:   int64(d.pieceSize),
		})
	} else {
		d.eventCh <- &probeResultEv{
			err: fmt.Errorf("disabled multi-threads downloading"),
		}
	}

	for {
		select {
		case obj := <-d.eventCh:
			// probeResultEv is guaranteed to be appeared before downloadDoneEv
			switch ev := obj.(type) {
			case *probeResultEv:
				logger.Debugf("[ant] probeResultEv: %+v, err: %v", ev, ev.err)
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
				logger.Debugf("[ant] downloadDoneEv: %+v, err: %v", ev, ev.err)
				runnings--
				if ev.err != nil {
					if ev.probing {
						break
					}
					if !multiThreads {
						d.finish(true)
						return
					}
					if !ev.temporary {
						d.finish(true)
						return
					} else {
						tolerance--
						if tolerance < 0 {
							d.finish(true)
							return
						}
						// TODO: skip downloaded range
						lastFailedRange = ev.reqRange
						break
					}
				}

				// check
				if ev.reqRange != wholeFile {
					d.mu.Lock()
					downloaded := d.downloaded.coveredRange(ev.reqRange.begin)
					d.mu.Unlock()
					if !(downloaded.begin == ev.reqRange.begin &&
						downloaded.end >= ev.reqRange.end) {
						logger.Errorf("downloaded range %+v doesn't cover %+v",
							downloaded, ev.reqRange)
						d.finish(true)
						return
					}
				}
			default:
				logger.Errorf("unknown event: %+v", obj)
			}
		case <-d.cancelCtx.Done():
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
					go d.download(false, reqRange)
				} else {
					break
				}
			}
		} else {
			if !downloadingWhole {
				runnings++
				go d.download(false, wholeFile)
				downloadingWhole = true
			}
		}
		if runnings == 0 {
			logger.Infof("[ant] download %s finished", d.url)
			d.finish(false)
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

func (d *Downloader) download(probing bool, r dataRange) {
	sendEv := func(ev interface{}) {
		select {
		case d.eventCh <- ev:
		case <-d.cancelCtx.Done():
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
	req, err := http.NewRequestWithContext(d.cancelCtx,
		http.MethodGet, d.url, nil)
	if err != nil {
		logger.Errorf("new request error: %s", err)
		feedback(err, false)
		return
	}
	if r != wholeFile {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.begin, r.end))
	}
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
		feedback(err, false)
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
		n, err := resp.Body.Read(buf)
		if n > 0 {
			werr := d.writeAt(buf[:n], offset)
			if werr != nil {
				logger.Errorf("write to file err: %s, offset: %d", werr, offset)
				d.finish(true)
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
