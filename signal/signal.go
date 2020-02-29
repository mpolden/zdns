package signal

import (
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// Reloader is the interface for types that need to act on a reload signal.
type Reloader interface {
	Reload()
}

// Handler represents a signal handler and holds references to types that should act on operating system signals.
type Handler struct {
	signal    chan os.Signal
	reloaders []Reloader
	closers   []io.Closer
	wg        sync.WaitGroup
}

// NewHandler creates a new handler for handling operating system signals.
func NewHandler(c chan os.Signal) *Handler {
	h := &Handler{signal: c}
	signal.Notify(h.signal)
	h.wg.Add(1)
	go h.readSignal()
	return h
}

// OnReload registers a reloader to call for the signal SIGHUP.
func (h *Handler) OnReload(r Reloader) { h.reloaders = append(h.reloaders, r) }

// OnClose registers a closer to call for signals SIGTERM and SIGINT.
func (h *Handler) OnClose(c io.Closer) { h.closers = append(h.closers, c) }

// Close stops handling any new signals and completes processing of pending signals before returning.
func (h *Handler) Close() error {
	signal.Stop(h.signal)
	close(h.signal)
	h.wg.Wait()
	return nil
}

func (h *Handler) readSignal() {
	defer h.wg.Done()
	for sig := range h.signal {
		switch sig {
		case syscall.SIGHUP:
			log.Printf("received signal %s: reloading", sig)
			for _, r := range h.reloaders {
				r.Reload()
			}
		case syscall.SIGTERM, syscall.SIGINT:
			log.Printf("received signal %s: shutting down", sig)
			for _, c := range h.closers {
				if err := c.Close(); err != nil {
					log.Printf("close of %T failed: %s", c, err)
				}
			}
		}
	}
}
