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
			if empty := u.CheckActive(1 * time.Minute); empty {
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

func (m *manager) AddPlaylist(id string, pl *playlist) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.playlistMap[id] = pl
}

func (m *manager) GetPlaylist(id string) *playlist {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.playlistMap[id]
}

func (m *manager) GetUser(id string) *user {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.userMap[id]
	if u == nil {
		logger.Infof("new user %s", id)
		u = newUser(id)
		m.userMap[id] = u
	}
	return u
}
