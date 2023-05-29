package hlsboost

import (
	"sync"
	"time"

	"github.com/zjx20/urlproxy/logger"
)

var (
	mgr  *manager
	once sync.Once
)

type manager struct {
	mu          sync.Mutex
	userMap     map[string]*user
	playlistMap map[string]*playlist
}

func globalManager() *manager {
	once.Do(func() {
		mgr = &manager{
			userMap:     make(map[string]*user),
			playlistMap: make(map[string]*playlist),
		}
		go mgr.tidyLoop()
	})
	return mgr
}

func (m *manager) tidyLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		for uId, u := range m.userMap {
			if active := u.CheckActive(1 * time.Minute); !active {
				delete(m.userMap, uId)
				logger.Infof("user %s becomes inactive", uId)
			}
		}
		for pId, pl := range m.playlistMap {
			// returns error if the playlist is still in used
			if err := pl.Destroy(); err == nil {
				delete(m.playlistMap, pId)
				logger.Infof("playlist %s becomes inactive", pId)
			}
		}
		m.mu.Unlock()
	}
}

func (m *manager) GetOrAddPlaylistAcquired(id string, pl *playlist) (*playlist, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ret, ok := m.playlistMap[id]
	if !ok {
		m.playlistMap[id] = pl
		ret = pl
	}
	ret.Acquire()
	return ret, !ok
}

func (m *manager) GetPlaylistAcquired(id string) *playlist {
	m.mu.Lock()
	defer m.mu.Unlock()
	pl := m.playlistMap[id]
	if pl != nil {
		pl.Acquire()
	}
	return pl
}

func (m *manager) GetUserAcquired(id string) *user {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.userMap[id]
	if u == nil {
		logger.Infof("new user %s", id)
		u = newUser(id)
		m.userMap[id] = u
	}
	u.Acquire()
	return u
}
