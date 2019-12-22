package signal

import (
	"io/ioutil"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/mpolden/zdns/log"
)

type reloaderCloser struct {
	reloaded bool
	closed   bool
}

func (rc *reloaderCloser) Reload() { rc.reloaded = true }
func (rc *reloaderCloser) Close() error {
	rc.closed = true
	return nil
}
func (rc *reloaderCloser) isReloaded() bool { return rc.reloaded }
func (rc *reloaderCloser) isClosed() bool   { return rc.closed }
func (rc *reloaderCloser) reset() {
	rc.reloaded = false
	rc.closed = false
}

func TestHandler(t *testing.T) {
	logger, err := log.New(ioutil.Discard, "", log.RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(make(chan os.Signal, 1), logger)

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
