package zdns

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mpolden/zdns/dns"
	"github.com/mpolden/zdns/hosts"
)

const (
	// HijackZero returns the zero IP address to matching requests.
	HijackZero = iota
	// HijackEmpty returns an empty answer to matching requests.
	HijackEmpty
	// HijackHosts returns the value of the  hoss entry to matching request.
	HijackHosts
)

// A Server defines parameters for running a DNS server.
type Server struct {
	Config  Config
	Logger  *log.Logger
	proxy   *dns.Proxy
	matcher *hosts.Matcher
	ticker  *time.Ticker
	done    chan bool
	signal  chan os.Signal
	mu      sync.RWMutex
}

// NewServer returns a new server configured according to config.
func NewServer(config Config) (*Server, error) {
	server := &Server{
		Config: config,
		signal: make(chan os.Signal, 1),
		done:   make(chan bool, 1),
	}
	if config.Filter.refreshInterval > 0 {
		server.ticker = time.NewTicker(config.Filter.refreshInterval)
		go server.reloadHosts()
	}
	signal.Notify(server.signal)
	go server.readSignal()
	proxy := dns.NewProxy(server.hijack, config.Resolvers, config.Resolver.timeout)
	server.proxy = proxy
	return server, nil
}

func readHosts(name string) (hosts.Hosts, error) {
	url, err := url.Parse(name)
	if err != nil {
		return nil, err
	}
	var rc io.ReadCloser
	switch url.Scheme {
	case "file":
		f, err := os.Open(url.Path)
		if err != nil {
			return nil, err
		}
		rc = f
	case "http", "https":
		client := http.Client{Timeout: 10 * time.Second}
		res, err := client.Get(url.String())
		if err != nil {
			return nil, err
		}
		rc = res.Body
	default:
		return nil, fmt.Errorf("%s: invalid scheme: %s", url, url.Scheme)
	}
	defer rc.Close()
	return hosts.Parse(rc)
}

func nonFqdn(s string) string {
	sz := len(s)
	if sz > 0 && s[sz-1:] == "." {
		return s[:sz-1]
	}
	return s
}

func (s *Server) logf(format string, v ...interface{}) {
	if s.Logger != nil {
		s.Logger.Printf(format, v...)
	}
}

func (s *Server) readSignal() {
	for {
		select {
		case <-s.done:
			signal.Stop(s.signal)
			return
		case sig := <-s.signal:
			switch sig {
			case syscall.SIGHUP:
				s.logf("received signal %s: reloading filters", sig)
				s.loadHosts()
			case syscall.SIGTERM, syscall.SIGINT:
				s.logf("received signal %s: shutting down", sig)
				s.Close()
			default:
				s.logf("received signal %s: ignoring", sig)
			}
		}
	}
}

func (s *Server) reloadHosts() {
	for {
		select {
		case <-s.done:
			s.ticker.Stop()
			return
		case <-s.ticker.C:
			s.loadHosts()
		}
	}
}

func (s *Server) loadHosts() {
	var hs []hosts.Hosts
	var size int
	for _, f := range s.Config.Filters {
		h, err := readHosts(f.URL)
		if err != nil {
			s.logf("failed to read hosts from %s: %s", f.URL, err)
			continue
		}
		if f.Reject {
			hs = append(hs, h)
			s.logf("loaded %d hosts from %s", len(h), f.URL)
			size += len(h)
		} else {
			var removed int
			for hostToRemove := range h {
				for _, h := range hs {
					if _, ok := h.Get(hostToRemove); ok {
						removed++
						h.Del(hostToRemove)
					}
				}
			}
			size -= removed
			if removed > 0 {
				s.logf("removed %d hosts from %s", len(h), f.URL)
			}
		}
	}
	m := hosts.NewMatcher(hs...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matcher = m
	s.logf("loaded %d hosts in total", size)
}

// Close terminates all active operations and shuts down the DNS server.
func (s *Server) Close() {
	s.done <- true
	s.done <- true
	if err := s.proxy.Close(); err != nil {
		s.logf("error during close: %s", err)
	}
}

func (s *Server) hijack(r *dns.Request) *dns.Reply {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.matcher.Match(nonFqdn(r.Name)) {
		return nil // No match
	}
	switch s.Config.Filter.hijackMode {
	case HijackZero:
		switch r.Type {
		case dns.TypeA:
			return dns.ReplyA(r.Name, net.IPv4zero)
		case dns.TypeAAAA:
			return dns.ReplyAAAA(r.Name, net.IPv6zero)
		}
	case HijackEmpty:
		return &dns.Reply{}
	case HijackHosts:
		// TODO: Provide answer from hosts
	}
	return nil
}

// ListenAndServe starts a server on configured address and protocol.
func (s *Server) ListenAndServe() error {
	return s.proxy.ListenAndServe(s.Config.Listen, s.Config.Protocol)
}
