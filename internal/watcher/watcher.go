package watcher

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch observes path and calls onChange after a debounce window.
// Caller must call Close when done.
type Watch struct {
	w     *fsnotify.Watcher
	stop  chan struct{}
	once  sync.Once
	onChg func()
	name  string
	delay time.Duration
}

func Start(path string, debounce time.Duration, onChange func()) (*Watch, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	name := filepath.Base(abs)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return nil, err
	}
	wt := &Watch{
		w:     w,
		stop:  make(chan struct{}),
		onChg: onChange,
		name:  name,
		delay: debounce,
	}
	go wt.loop()
	return wt, nil
}

func (wt *Watch) loop() {
	var timer *time.Timer
	var timerC <-chan time.Time

	reset := func() {
		if timer == nil {
			timer = time.NewTimer(wt.delay)
			timerC = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wt.delay)
		timerC = timer.C
	}

	for {
		select {
		case <-wt.stop:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-timerC:
			timerC = nil
			wt.onChg()
		case ev, ok := <-wt.w.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != wt.name {
				continue
			}
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Rename) || ev.Has(fsnotify.Remove) {
				reset()
			}
		case _, ok := <-wt.w.Errors:
			if !ok {
				return
			}
		}
	}
}

func (wt *Watch) Close() {
	wt.once.Do(func() {
		close(wt.stop)
		_ = wt.w.Close()
	})
}
