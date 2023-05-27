package hlsboost

import (
	"context"
	"fmt"
	"path"
	"sync"

	"github.com/zjx20/urlproxy/ant"
	"github.com/zjx20/urlproxy/logger"
	"github.com/zjx20/urlproxy/urlopts"
)

const (
	defaultPieceSize = 512 * 1024
	defaultAnts      = 5
)

type segment struct {
	mu         sync.Mutex
	seq        int
	segId      string
	downloader *ant.Downloader

	started   bool
	interests int
	dying     bool
}

func newSegment(seq int, segId string, url string, cacheDir string,
	reqOpts *urlopts.Options) *segment {
	pieceSize, ok := urlopts.OptAntPieceSize.ValueFrom(reqOpts)
	if !ok {
		pieceSize = defaultPieceSize
	}
	ants, ok := urlopts.OptAntConcurrentPieces.ValueFrom(reqOpts)
	if !ok {
		ants = defaultAnts
	}
	d := ant.NewDownloader(int(pieceSize), int(ants), url,
		path.Join(cacheDir, segId), manipulateRequestToSkipHlsBoost)
	s := &segment{
		seq:        seq,
		segId:      segId,
		downloader: d,
	}
	return s
}

func (s *segment) ReadAt(ctx context.Context, b []byte, off int64) (n int, err error) {
	return s.downloader.ReadAt(ctx, b, off)
}

func (s *segment) TotalSize(ctx context.Context) (int64, error) {
	err := s.downloader.WaitReady(ctx, 0)
	if err != nil {
		return -1, err
	}
	_, total := s.downloader.Status()
	return total, nil
}

func (s *segment) Prefetch() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		// last time of downloading is failed if Retry() returns no error
		if err := s.downloader.Retry(); err != nil {
			return false
		}
	}
	s.started = true
	err := s.downloader.Start()
	if err != nil {
		logger.Errorf("prefetch segment %s failed: %v", s.segId, err)
		return false
	}
	return true
}

func (s *segment) Destroy(lazy bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.interests > 0 {
		if lazy {
			s.dying = true
			return nil
		}
		return fmt.Errorf("still in use")
	}
	s.releaseLocked()
	return nil
}

func (s *segment) releaseLocked() {
	s.downloader.Destroy()
}

func (s *segment) AddCompletionListener(ch chan struct{}) {
	s.downloader.AddCompletionListener(ch)
}

func (s *segment) Status() ant.Status {
	status, _ := s.downloader.Status()
	return status
}

func (s *segment) Acquire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interests++
}

func (s *segment) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interests--
	if s.interests == 0 && s.dying {
		s.releaseLocked()
	}
	if s.interests < 0 {
		logger.Errorf("interests(%d) of segment %s become negative",
			s.interests, s.segId)
	}
}
