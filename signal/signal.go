package signal

import (
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// Reloader is the interface for types that need to act on a reload signal.
type Reloader interface {
	Reload()
}

// Handler represents a signal handler and holds references to types that should act on operating system signals.
type Handler struct {
	logger    *log.Logger
	signal    chan os.Signal
	reloaders []Reloader
	closers   []io.Closer
}

// NewHandler creates a new handler for handling operating system signals.
func NewHandler(c chan os.Signal, logger *log.Logger) *Handler {
	h := &Handler{logger: logger, signal: c}
	signal.Notify(h.signal)
	go h.readSignal()
	return h
}

// OnReload registers a reloader to call for the signal SIGHUP.
func (s *Handler) OnReload(r Reloader) { s.reloaders = append(s.reloaders, r) }

// OnClose registers a closer to call for signals SIGTERM and SIGINT.
func (s *Handler) OnClose(c io.Closer) { s.closers = append(s.closers, c) }

func (s *Handler) readSignal() {
	for sig := range s.signal {
		switch sig {
		case syscall.SIGHUP:
			s.logger.Printf("received signal %s: reloading", sig)
			for _, r := range s.reloaders {
				r.Reload()
			}
		case syscall.SIGTERM, syscall.SIGINT:
			signal.Stop(s.signal)
			s.logger.Printf("received signal %s: shutting down", sig)
			for _, c := range s.closers {
				if err := c.Close(); err != nil {
					s.logger.Printf("close failed: %s", err)
				}
			}

		default:
			s.logger.Printf("received signal %s: ignoring", sig)
		}
	}
}
