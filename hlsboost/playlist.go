package hlsboost

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/etherlabsio/go-m3u8/m3u8"
	"github.com/zjx20/urlproxy/ant"
	"github.com/zjx20/urlproxy/logger"
	"github.com/zjx20/urlproxy/urlopts"
)

// The logic of playlist serving:
//   1. The proxy automatically updates the playlist in the background until
//      no client plays the stream.
//   2. Track the playback progress of each client by monitoring the fetch
//      events of playlist and segment.
//   3. Client requests the playlist:
//     a. If the client pulls for the first time, returns the latest 10
//        segments in the playlist;
//     b. If the client updates the playlist, and if the client playback is
//        active (meaning there is always a segment pull action), then return
//        the adjacent segments according to the progress, otherwise return
//        the latest 10 segments in the playlist;
//     c. Fault update: If the client progress is more than 10 minutes
//        different from the latest segment of the playlist, force to return
//        the latest segment.
//   4. Playlist shrinkage: The segments before the client progress can
//      theoretically be deleted, because they are invisible to the client;
//      however, for safety reasons, only the segments before the client
//      progress by 5 minutes are deleted.
//   5. Upstream error: If an error such as 404/410 occurs when downloading
//      a segment from upstream, it may be that the playlist is severely
//      outdated, in which case we need to clear the client progress, so that
//      the client can pull the latest playlist.

const (
	defaultFetchTimeout  = 5 * time.Second
	maxUpdateIntervalSec = 10
)

type prefetch struct {
	startFromSeq int
	durationSec  float64
}

type playlist struct {
	mu            sync.Mutex
	id            string
	selfCli       *SelfClient
	uri           string
	reqOpts       *urlopts.Options
	updateIntvl   int32 // in seconds
	fetchTimeout  time.Duration
	maxPrefetches int
	cancelCtx     context.Context
	cancel        context.CancelFunc
	cacheRoot     string
	cacheDir      string

	m3         *m3u8.Playlist
	segments   []*segment
	prefetches []*prefetch
	notifyCh   chan struct{}
	interests  int
}

func newPlaylist(selfCli *SelfClient, uri string, reqOpts *urlopts.Options,
	cacheRoot string) *playlist {
	cancelCtx, cancel := context.WithCancel(context.Background())
	maxPrefetches, ok := urlopts.OptHLSPrefetches.ValueFrom(reqOpts)
	if !ok {
		// libmpv will request two segments at the same time, but it needs
		// both segments to return data. If the second segment does not return
		// data, it will not consume the data of the first segment.
		// Therefore, the minimum number of concurrent prefetches is 2,
		// otherwise libmpv will only start playing after the first segment
		// is fully downloaded, because we start download the second segment
		// after that.
		maxPrefetches = 3
	}
	timeoutMs, ok := urlopts.OptHLSTimeoutMs.ValueFrom(reqOpts)
	if !ok {
		timeoutMs = int64(defaultFetchTimeout / time.Millisecond)
	}
	return &playlist{
		id:            md5Short(uri),
		selfCli:       selfCli,
		uri:           uri,
		reqOpts:       reqOpts,
		updateIntvl:   maxUpdateIntervalSec,
		fetchTimeout:  time.Duration(timeoutMs) * time.Millisecond,
		maxPrefetches: int(maxPrefetches),
		cancelCtx:     cancelCtx,
		cancel:        cancel,
		cacheRoot:     cacheRoot,
		notifyCh:      make(chan struct{}, 1),
	}
}

func (p *playlist) Init(m3 *m3u8.Playlist) error {
	cacheDir, err := makeCacheDirForPlaylist(p.id, p.cacheRoot)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cacheDir = cacheDir
	p.resetLocked(m3)
	go p.runLoop()
	return nil
}

func (p *playlist) Destroy() error {
	p.tryClear()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interests > 0 || len(p.segments) > 0 {
		return fmt.Errorf("still in use")
	}
	if p.cancelCtx.Err() != nil {
		// already destroyed
		return nil
	}
	p.cancel()
	return nil
}

func (p *playlist) GetNewestSegments(count int) *m3u8.Playlist {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.getNewestSegmentsLocked(count)
}

func (p *playlist) getNewestSegmentsLocked(count int) *m3u8.Playlist {
	pl := *p.m3
	if len(pl.Items) > count {
		off := len(pl.Items) - count
		pl.Items = append([]m3u8.Item{}, pl.Items[off:]...)
		pl.Sequence += off
	} else {
		pl.Items = append([]m3u8.Item{}, pl.Items...)
	}
	return &pl
}

func (p *playlist) GetSegmentsFrom(seq int, count int) *m3u8.Playlist {
	p.mu.Lock()
	defer p.mu.Unlock()
	end := p.m3.Sequence + len(p.m3.Items)
	if seq < p.m3.Sequence || seq >= end {
		// out of range
		return p.getNewestSegmentsLocked(count)
	}
	off := seq - p.m3.Sequence
	tail := off + count
	if tail > len(p.m3.Items) {
		tail = len(p.m3.Items)
	}
	pl := *p.m3
	pl.Items = append([]m3u8.Item{}, pl.Items[off:tail]...)
	pl.Sequence += off
	return &pl
}

func (p *playlist) GetSegment(segId string) *segment {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, seg := range p.segments {
		if seg == nil {
			continue
		}
		if seg.segId == segId {
			return seg
		}
	}
	return nil
}

func (p *playlist) Prefetch(nextToSeq int, durationSec float64) (handle any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	end := p.m3.Sequence + p.m3.ItemSize()
	if nextToSeq < p.m3.Sequence || nextToSeq >= end {
		// out of range
		return nil
	}
	pf := &prefetch{
		startFromSeq: nextToSeq,
		durationSec:  durationSec,
	}
	p.prefetches = append(p.prefetches, pf)
	select {
	case p.notifyCh <- struct{}{}:
	default:
	}
	return pf
}

func (p *playlist) StopPrefetch(handle any) {
	pf, ok := handle.(*prefetch)
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := -1
	for i, x := range p.prefetches {
		if pf == x {
			idx = i
			break
		}
	}
	if idx != -1 {
		p.prefetches = append(p.prefetches[:idx], p.prefetches[idx+1:]...)
	}
}

func (p *playlist) runLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	var last time.Time
	for {
		select {
		case <-ticker.C:
			interval := time.Duration(atomic.LoadInt32(&p.updateIntvl)) * time.Second
			if time.Since(last) < interval {
				continue
			}
			last = time.Now()
			go func() {
				times := 3
				for times > 0 {
					if err := p.update(); err != nil {
						times--
						continue
					}
					p.tryShrink()
					p.tryPrefetch()
					break
				}
			}()

		case <-p.notifyCh:
			p.tryPrefetch()

		case <-p.cancelCtx.Done():
			return
		}
	}
}

func (p *playlist) tryPrefetch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fetchings := 0
	for _, seg := range p.segments {
		status := seg.Status()
		if ant.IsStarted(status) {
			fetchings++
		}
	}
	if fetchings >= p.maxPrefetches {
		logger.Debugf("playlist %s has %d fetchings, max %d, skip prefetch",
			p.id, fetchings, p.maxPrefetches)
		return
	}
	var pending []*segment
	for _, pf := range p.prefetches {
		sz := p.m3.ItemSize()
		idx := pf.startFromSeq - p.m3.Sequence
		dur := float64(0)
		logger.Debugf("playlist %s prefetch intention %+v, seq: %d, idx: %d",
			p.id, pf, p.m3.Sequence, idx)
		for (idx >= 0 && idx < sz) && dur < pf.durationSec {
			status := p.segments[idx].Status()
			// retry for failed segments, but ignore the one is currently
			// requested by the client (idx == 0), because it's too late
			// to prefetch.
			if status == ant.NotStarted || (idx > 0 && status == ant.Aborted) {
				pending = append(pending, p.segments[idx])
			}
			dur += p.m3.Items[idx].(*m3u8.SegmentItem).Duration
			idx++
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].seq < pending[j].seq
	})
	logger.Debugf("playlist %s has %d pending prefetches", p.id, len(pending))
	var last *segment
	idx := 0
	for fetchings < p.maxPrefetches && idx < len(pending) {
		// the pending list may contain duplicated items
		if pending[idx] != last {
			if pending[idx].Prefetch() {
				pending[idx].AddCompletionListener(p.notifyCh)
				fetchings++
			}
		}
		last = pending[idx]
		idx++
	}
}

func (p *playlist) update() error {
	ctx, cancel := context.WithTimeout(p.cancelCtx, p.fetchTimeout)
	defer cancel()
	resp, err := p.selfCli.Get(ctx, "/", p.uri, p.reqOpts)
	if err != nil {
		logger.Errorf("failed to get m3u8 playlist from %s, err: %s",
			p.uri, err)
		return err
	}
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		logger.Errorf("bad http status code %d from %s",
			resp.StatusCode, p.uri)
		return err
	}
	pl, err := m3u8.Read(resp.Body)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err != nil {
		logger.Errorf("parse m3u8 playlist from %s failed, err: %s",
			p.uri, err)
		return err
	}
	if pl.IsMaster() {
		logger.Errorf("wanted MEDIA m3u8 playlist from %s", p.uri)
		return fmt.Errorf("it's a MASTER playlist")
	}
	p.merge(pl)
	return nil
}

func (p *playlist) filterOut(m3 *m3u8.Playlist) {
	// filter unsupported items
	var filtered []m3u8.Item
	for _, it := range m3.Items {
		if _, ok := it.(*m3u8.SegmentItem); !ok {
			logger.Warnf("unsupported item %+v from m3u8", it)
		} else {
			filtered = append(filtered, it)
		}
	}
	m3.Items = filtered
}

func (p *playlist) merge(m3 *m3u8.Playlist) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelCtx.Err() != nil {
		// already destroyed
		return
	}
	p.filterOut(m3)

	end1 := p.m3.Sequence + p.m3.ItemSize()
	end2 := m3.Sequence + m3.ItemSize()
	if end1 >= end2 {
		// pl is a sub set of the current list
		if end1-end2 > 3 {
			logger.Errorf("playlist %s returned a stale list, end1: %d, end2: %d",
				p.id, end1, end2)
		} else {
			logger.Infof("playlist %s has no update", p.id)
		}
		return
	}
	tails := end2 - end1
	if tails > m3.ItemSize() {
		logger.Warnf("reset playlist %s, lag too much, tails: %d, size: %d",
			p.id, tails, m3.ItemSize())
		p.resetLocked(m3)
		return
	}
	news := m3.Items[m3.ItemSize()-tails:]
	p.appendItemsLocked(news)
	logger.Infof("playlist %s add %d new items", p.id, tails)
}

func (p *playlist) appendItemsLocked(news []m3u8.Item) {
	for _, it := range news {
		segItem := it.(*m3u8.SegmentItem)
		seq := p.m3.Sequence + p.m3.ItemSize()
		segId := md5Short(segItem.Segment)
		url := p.selfCli.ToFinalUrl(p.uri, segItem.Segment, p.reqOpts)
		segment := newSegment(seq, segId, url, p.cacheDir, p.reqOpts)
		p.segments = append(p.segments, segment)
		p.m3.AppendItem(it)
	}
	if len(news) > 0 {
		select {
		case p.notifyCh <- struct{}{}:
		default:
		}
	}
}

func (p *playlist) resetLocked(m3 *m3u8.Playlist) {
	p.filterOut(m3)
	updateInterval := 2 * m3.Target
	if updateInterval > int(maxUpdateIntervalSec) {
		updateInterval = maxUpdateIntervalSec
	}
	if p.updateIntvl != int32(updateInterval) {
		logger.Infof("playlist %s, updateIntvl changed from %d to %d",
			p.id, p.updateIntvl, updateInterval)
		atomic.StoreInt32(&p.updateIntvl, int32(updateInterval))
	}
	clone := *m3
	clone.Items = nil // will append later

	for _, seg := range p.segments {
		// lazy destroy
		seg.Destroy(true)
	}
	p.segments = nil

	p.m3 = &clone
	p.appendItemsLocked(m3.Items)
}

func (p *playlist) shrinkLocked(max int) {
	cnt := 0
	for i := 0; i < max; i++ {
		err := p.segments[i].Destroy(false)
		if err != nil {
			break
		}
		cnt++
	}
	p.segments = p.segments[cnt:]
	p.m3.Items = p.m3.Items[cnt:]
	p.m3.Sequence += cnt
}

// shrink segments before the earliest one that tagged to be prefetched
func (p *playlist) tryShrink() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelCtx.Err() != nil {
		// already destroyed
		return
	}
	end := p.m3.Sequence + p.m3.ItemSize()
	minSeq := end
	for _, pf := range p.prefetches {
		if minSeq > pf.startFromSeq {
			minSeq = pf.startFromSeq
		}
	}
	const minNumOfSegs = 10
	const keepBefore = 5
	cutPos := minSeq - p.m3.Sequence - keepBefore
	if cutPos > p.m3.ItemSize()-minNumOfSegs {
		cutPos = p.m3.ItemSize() - minNumOfSegs
	}
	if cutPos < 0 {
		// nothing to shrink
		return
	}
	p.shrinkLocked(cutPos)
}

func (p *playlist) tryClear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interests > 0 {
		return
	}
	p.shrinkLocked(p.m3.ItemSize())
}

func (p *playlist) Acquire() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interests++
}

func (p *playlist) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interests--
}
