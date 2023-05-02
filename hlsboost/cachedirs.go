package hlsboost

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zjx20/urlproxy/logger"

	"github.com/zbiljic/go-filelock"
)

const (
	lockFile          = "hlsboost.lock"
	playlistDirPrefix = "pl-"
)

var (
	onceClear sync.Once
	locksMu   sync.Mutex
	locks     = map[string]filelock.TryLockerSafe{}
)

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func tryLock(lockPath string) (filelock.TryLockerSafe, error) {
	absPath, err := filepath.Abs(lockPath)
	if err != nil {
		return nil, err
	}
	fl, err := filelock.New(absPath)
	if err != nil {
		return nil, err
	}
	ok, err := fl.TryLock()
	if err != nil || !ok {
		return nil, err
	}
	return fl, nil
}

func clearStaleDirs(cacheDir string) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		logger.Errorf("failed to ReadDir, path: %s, err: %s", cacheDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), playlistDirPrefix) {
			continue
		}
		lockPath := path.Join(cacheDir, e.Name(), lockFile)
		if !fileExists(lockPath) {
			continue
		}
		fl, _ := tryLock(lockPath)
		if fl != nil {
			// lock can successful, implying that the process
			// which created this folder has died.
			dir := path.Join(cacheDir, e.Name())
			os.RemoveAll(dir)
			logger.Infof("cache dir %s has been deleted", dir)
			fl.Destroy()
		}
	}
}

func makeCacheDirForPlaylist(playlistId string, cacheDir string) (string, error) {
	onceClear.Do(func() {
		clearStaleDirs(cacheDir)
	})
	dir := fmt.Sprintf("%s%s-%d", playlistDirPrefix, playlistId, os.Getpid())
	dirPath := path.Join(cacheDir, dir)
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		logger.Errorf("failed to create cache dir: %s, err: %s", dirPath, err)
		return "", err
	}
	locksMu.Lock()
	defer locksMu.Unlock()
	if _, exists := locks[dirPath]; exists {
		return dirPath, nil
	}
	lockPath := path.Join(dirPath, lockFile)
	fl, err := tryLock(lockPath)
	if err != nil {
		logger.Errorf("failed to create lock for dir: %s, err: %s", dirPath, err)
		return "", err
	}
	logger.Infof("created lock %s", lockPath)
	locks[dirPath] = fl
	return dirPath, nil
}
