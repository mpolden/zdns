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

	"github.com/mpolden/zdns/hosts"

	"github.com/miekg/dns"
)

// A Server defines parameters for running a DNS server.
type Server struct {
	Config  Config
	Logger  *log.Logger
	mu      sync.RWMutex
	server  *dns.Server
	client  *dns.Client
	matcher *hosts.Matcher
	ticker  *time.Ticker
	done    chan bool
	signal  chan os.Signal
}

func readHosts(url *url.URL) (hosts.Hosts, error) {
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

// NewServer returns a new server configured according to config.
func NewServer(config Config) (*Server, error) {
	done := make(chan bool, 1)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig)
	var ticker *time.Ticker
	if config.Filter.RefreshInterval.Duration > 0 {
		ticker = time.NewTicker(config.Filter.RefreshInterval.Duration)
	}
	server := &Server{
		Config: config,
		ticker: ticker,
		signal: sig,
		done:   done,
		client: &dns.Client{Timeout: config.ResolverTimeout.Duration},
	}
	if ticker != nil {
		go server.reloadHosts()
	}
	go server.readSignal()
	return server, nil
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
	for sig := range s.signal {
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

func (s *Server) reloadHosts() {
	for {
		select {
		case <-s.ticker.C:
			s.loadHosts()
		case <-s.done:
			s.ticker.Stop()
			return
		}
	}
}

func (s *Server) loadHosts() {
	var hs []hosts.Hosts
	var size int
	for _, filter := range s.Config.Filters {
		h, err := readHosts(filter.URL.URL)
		if err != nil {
			s.logf("failed to read hosts from %s: %s", filter.URL.URL, err)
			continue
		}
		if filter.Reject {
			hs = append(hs, h)
			s.logf("loaded %d hosts from %s", len(h), filter.URL.URL)
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
				s.logf("removed %d hosts from %s", len(h), filter.URL.URL)
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
	if s.server != nil {
		if err := s.server.Shutdown(); err != nil {
			s.logf("error during shutdown: %s", err)
		}
	}
}

func (s *Server) reply(r *dns.Msg) *dns.Msg {
	if len(r.Question) != 1 {
		return nil
	}
	name := r.Question[0].Name
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.matcher.Match(nonFqdn(name)) {
		return nil // No match
	}
	m := dns.Msg{}
	switch s.Config.Filter.RejectMode {
	case "zero":
		switch r.Question[0].Qtype {
		case dns.TypeA:
			m.Answer = []dns.RR{&dns.A{
				A:   net.IPv4zero,
				Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
			}}
		case dns.TypeAAAA:
			m.Answer = []dns.RR{&dns.AAAA{
				AAAA: net.IPv6zero,
				Hdr:  dns.RR_Header{Name: name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 3600},
			}}
		}
	case "no-data":
		// Nothing to do
	case "hosts":
		// TODO: Provide answer from hosts
	}
	// Pretend this is an recursive answer
	m.RecursionAvailable = true
	m.SetReply(r)
	return &m
}

// ServeDNS implements the dns.Handler interface.
func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	if s.matcher == nil {
		s.loadHosts()
	}
	reply := s.reply(r)
	if reply != nil {
		s.logf("blocking host %q", r.Question[0].Name)
		w.WriteMsg(reply)
		return
	}
	for _, resolver := range s.Config.Resolvers {
		rr, _, err := s.client.Exchange(r, resolver.Name)
		if err != nil {
			s.logf("query failed: %s", err)
			continue
		}
		w.WriteMsg(rr)
		return
	}
	dns.HandleFailed(w, r)
}

// ListenAndServe listens on the network address addr and uses the server to process requests.
func (s *Server) ListenAndServe(addr string, network string) error {
	s.server = &dns.Server{Addr: addr, Net: network, Handler: s}
	return s.server.ListenAndServe()
}
