package dns

import (
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mpolden/zdns"

	"github.com/miekg/dns"
)

// A Server defines parameters for running a DNS server.
type Server struct {
	Config        zdns.Config
	Logger        *log.Logger
	filters       []*filterList
	rejectedHosts map[string]bool
	mu            sync.RWMutex
	server        *dns.Server
	client        *dns.Client
	ticker        *time.Ticker
	done          chan bool
	signal        chan os.Signal
}

// NewServer returns a new server configured according to config.
func NewServer(config zdns.Config) (*Server, error) {
	var filters []*filterList
	for _, f := range config.Filters {
		fl, err := newFilterList(f.URL.URL, f.Reject)
		if err != nil {
			return nil, err
		}
		filters = append(filters, fl)
	}

	done := make(chan bool, 1)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig)
	ticker := time.NewTicker(config.Filter.RefreshInterval.Duration)

	server := &Server{
		Config:  config,
		filters: filters,
		ticker:  ticker,
		signal:  sig,
		done:    done,
		client:  &dns.Client{Timeout: config.ResolverTimeout.Duration},
	}

	go server.reloadFilters()
	go server.readSignal()
	return server, nil
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
			s.loadFilters()
		case syscall.SIGTERM, syscall.SIGINT:
			s.logf("received signal %s: shutting down", sig)
			s.Close()
		default:
			s.logf("received signal %s: ignoring", sig)
		}
	}
}

func (s *Server) reloadFilters() {
	for {
		select {
		case <-s.ticker.C:
			s.loadFilters()
		case <-s.done:
			s.ticker.Stop()
			return
		}
	}
}

func (s *Server) loadFilters() {
	rejectedHosts := make(map[string]bool)
	for _, f := range s.filters {
		filter, err := f.Load()
		if err != nil {
			s.logf("failed to load filters from %s: %s", f.URL, err)
			continue
		}
		var loaded int
		for host, action := range filter {
			host = dns.Fqdn(host)
			if _, ok := rejectedHosts[host]; ok {
				s.logf("ignoring %q from %s: already filtered", host, f.URL)
				continue
			}
			loaded++
			rejectedHosts[host] = action
		}
		s.logf("loaded %d/%d hosts from %s", loaded, len(filter), f.URL)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejectedHosts = rejectedHosts
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
	if !s.rejectedHosts[name] {
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
	}
	// Pretend this is an recursive answer
	m.RecursionAvailable = true
	m.SetReply(r)
	return &m
}

// ServeDNS implements the dns.Handler interface.
func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
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
	if len(s.rejectedHosts) == 0 {
		s.loadFilters()
	}
	s.server = &dns.Server{Addr: addr, Net: network, Handler: s}
	return s.server.ListenAndServe()
}
