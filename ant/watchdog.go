package ant

import (
	"time"

	"github.com/zjx20/urlproxy/logger"
)

type watchDog struct {
	intrFunc func()
	interval time.Duration
	ch       chan interface{}
}

func newWatchDog(intrFunc func(), interval time.Duration) *watchDog {
	w := &watchDog{
		intrFunc: intrFunc,
		interval: interval,
		ch:       make(chan interface{}, 1),
	}
	go w.start()
	return w
}

func (w *watchDog) start() {
	for {
		select {
		case <-time.After(w.interval):
			logger.Errorf("watch dog timeout")
			w.intrFunc()
			return
		case item := <-w.ch:
			if item == nil {
				return
			}
			continue
		}
	}
}

func (w *watchDog) stop() {
	w.ch <- nil
}

func (w *watchDog) feed() {
	select {
	case w.ch <- true:
	default:
	}
}
