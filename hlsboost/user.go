package hlsboost

import (
	"sync"
	"time"

	"github.com/zjx20/urlproxy/logger"

	"github.com/etherlabsio/go-m3u8/m3u8"
)

// Prefetch strategy:
// 1. Prefetch based on client playback progress: prefetch a certain amount of
//    time ahead of the current progress, and stop prefetching when the client
//    stops playing to avoid consuming resources in the background.
// 2. Cache retention policy: segments after the client's progress will never
//    be deleted, while segments before it will be retained only up to a
//    certain number (e.g. five).
// 3. Dynamically adjust prefetch duration to prevent resource waste
//    when clients switch channels:
//    a. For first 30 seconds: cache 5s
//    b. For more than 30 seconds but less than one minute: cache 10s
//    c. For more than one minute but less than five minutes: cache 20s
//    d. For more than five minutes: cache 30s

type stream struct {
	userId         string
	pl             *playlist
	latestSeq      int
	startTime      time.Time
	lastTime       time.Time
	prefetchDurSec int
	prefetchHandle any
}

func (s *stream) update(seq int) {
	s.lastTime = time.Now()
	dur := s.lastTime.Sub(s.startTime)
	// dynamically adjust the prefetch duration:
	//   the longer the client play time, the longer
	//   the buffer duration.
	var prefetchDur int
	if dur < 30*time.Second {
		prefetchDur = 5
	} else if dur < 1*time.Minute {
		prefetchDur = 10
	} else if dur < 5*time.Minute {
		prefetchDur = 20
	} else {
		prefetchDur = 30
	}
	updatePrefetch := false
	if s.prefetchDurSec != prefetchDur {
		s.prefetchDurSec = prefetchDur
		updatePrefetch = true
		logger.Infof("user %s playlist %s, set prefetch duration to %ds",
			s.userId, s.pl.id, s.prefetchDurSec)
	}
	if s.latestSeq < seq {
		s.latestSeq = seq
		updatePrefetch = true
	}
	if updatePrefetch && s.latestSeq != -1 {
		s.startPrefetch(s.latestSeq)
	}
}

func (s *stream) setPlaylist(pl *playlist) {
	if s.pl == pl {
		return
	}
	if s.pl != nil {
		s.pl.StopPrefetch(s.prefetchHandle)
		s.pl.Release()
		s.prefetchHandle = nil
	}
	s.pl = pl
	if s.pl != nil {
		s.pl.Acquire()
		if s.latestSeq != -1 {
			s.startPrefetch(s.latestSeq)
		}
	}
}

func (s *stream) startPrefetch(seq int) {
	s.pl.StopPrefetch(s.prefetchHandle)
	s.prefetchHandle = s.pl.Prefetch(seq, float64(s.prefetchDurSec))
	logger.Debugf("user %s playlist %s, prefetch seq: %d, duration: %ds",
		s.userId, s.pl.id, seq, s.prefetchDurSec)
}

type user struct {
	mu        sync.Mutex
	id        string
	streamMap map[string]*stream // playlistId => *stream
}

func newUser(id string) *user {
	return &user{
		id:        id,
		streamMap: make(map[string]*stream),
	}
}

func (u *user) GetM3U8(pl *playlist) *m3u8.Playlist {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.streamMap[pl.id]
	if s == nil {
		logger.Infof("user %s start watch playlist %s", u.id, pl.id)
		s = &stream{
			userId:    u.id,
			latestSeq: -1,
			startTime: time.Now(),
		}
		u.streamMap[pl.id] = s
	}
	s.setPlaylist(pl)
	s.update(s.latestSeq) // update lastTime
	return pl.GetSegmentsFrom(s.latestSeq, 10)
}

func (u *user) GetSegment(pl *playlist, segId string) *segment {
	u.mu.Lock()
	defer u.mu.Unlock()
	seg := pl.GetSegment(segId)
	if seg == nil {
		logger.Warnf("user %s, segment %s not found from playlist %s",
			u.id, segId, pl.id)
		return nil
	}
	s := u.streamMap[pl.id]
	if s != nil {
		logger.Debugf("user %s playlist %s, get segment seq: %d",
			u.id, pl.id, seg.seq)
		s.update(seg.seq)
	} else {
		logger.Warnf("user %s get segment %s before getting the playlist %s",
			u.id, segId, pl.id)
	}
	return seg
}

func (u *user) ResetProgress(playlistId string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.streamMap[playlistId]
	if s != nil {
		s.latestSeq = -1
	}
}

func (u *user) CheckActive(timeout time.Duration) (empty bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for pId, s := range u.streamMap {
		if time.Since(s.lastTime) > timeout {
			s.setPlaylist(nil) // stop prefetch
			delete(u.streamMap, pId)
			logger.Infof("user %s stop watch playlist %s", u.id, pId)
		}
	}
	return len(u.streamMap) == 0
}
