package signal

import (
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

type reloaderCloser struct {
	mu       sync.RWMutex
	reloaded bool
	closed   bool
}

func (rc *reloaderCloser) Reload() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.reloaded = true
}
func (rc *reloaderCloser) Close() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.closed = true
	return nil
}
func (rc *reloaderCloser) isReloaded() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.reloaded
}
func (rc *reloaderCloser) isClosed() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.closed
}

func (rc *reloaderCloser) reset() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.reloaded = false
	rc.closed = false
}

func TestHandler(t *testing.T) {
	h := NewHandler(make(chan os.Signal, 1))

	rc := &reloaderCloser{}
	h.OnReload(rc)
	h.OnClose(rc)

	var tests = []struct {
		signal syscall.Signal
		value  func() bool
	}{
		{syscall.SIGHUP, rc.isReloaded},
		{syscall.SIGTERM, rc.isClosed},
		{syscall.SIGINT, rc.isClosed},
	}

	for _, tt := range tests {
		rc.reset()
		h.signal <- tt.signal
		ts := time.Now()
		for !tt.value() {
			time.Sleep(10 * time.Millisecond)
			if time.Since(ts) > 2*time.Second {
				t.Fatalf("timed out waiting for handler of signal %s", tt.signal)
			}
		}
	}
}
