package zdns

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
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
	Config     Config
	hosts      hosts.Hosts
	proxy      *dns.Proxy
	done       chan bool
	mu         sync.RWMutex
	httpClient *http.Client
}

// NewServer returns a new server configured according to config.
func NewServer(proxy *dns.Proxy, config Config) (*Server, error) {
	server := &Server{
		Config:     config,
		done:       make(chan bool, 1),
		proxy:      proxy,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	proxy.Handler = server.hijack

	// Periodically refresh hosts
	if interval := config.DNS.refreshInterval; interval > 0 {
		go server.reloadHosts(interval)
	}

	// Load initial hosts
	go server.loadHosts()
	return server, nil
}

func (s *Server) httpGet(url string) (io.ReadCloser, error) {
	var body io.ReadCloser
	policy := backoff.NewExponentialBackOff()
	policy.MaxInterval = 2 * time.Second
	policy.MaxElapsedTime = 30 * time.Second
	err := backoff.Retry(func() error {
		res, err := s.httpClient.Get(url)
		if err == nil {
			body = res.Body
		}
		return err
	}, policy)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (s *Server) readHosts(name string) (hosts.Hosts, error) {
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
		rc, err = s.httpGet(url.String())
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%s: invalid scheme: %s", url, url.Scheme)
	}
	hosts, err := hosts.Parse(rc)
	if err1 := rc.Close(); err == nil {
		err = err1
	}
	return hosts, err
}

func nonFqdn(s string) string {
	sz := len(s)
	if sz > 0 && s[sz-1:] == "." {
		return s[:sz-1]
	}
	return s
}

func (s *Server) reloadHosts(interval time.Duration) {
	for {
		select {
		case <-s.done:
			return
		case <-time.After(interval):
			s.loadHosts()
		}
	}
}

func (s *Server) loadHosts() {
	hs := make(hosts.Hosts)
	for _, h := range s.Config.Hosts {
		src := "inline hosts"
		hs1 := h.hosts
		if h.URL != "" {
			src = h.URL
			var err error
			hs1, err = s.readHosts(h.URL)
			if err != nil {
				log.Printf("failed to read hosts from %s: %s", h.URL, err)
				continue
			}
		}
		if h.Hijack {
			for name, ipAddrs := range hs1 {
				hs[name] = ipAddrs
			}
			log.Printf("loaded %d hosts from %s", len(hs1), src)
		} else {
			removed := 0
			for hostToRemove := range hs1 {
				if _, ok := hs.Get(hostToRemove); ok {
					removed++
					hs.Del(hostToRemove)
				}
			}
			if removed > 0 {
				log.Printf("removed %d hosts from %s", removed, src)
			}
		}
	}
	s.mu.Lock()
	s.hosts = hs
	s.mu.Unlock()
	log.Printf("loaded %d hosts in total", len(hs))
}

// Reload updates hosts entries of Server s.
func (s *Server) Reload() { s.loadHosts() }

// Close terminates all active operations and shuts down the DNS server.
func (s *Server) Close() error {
	s.done <- true
	return nil
}

func (s *Server) hijack(r *dns.Request) *dns.Reply {
	if r.Type != dns.TypeA && r.Type != dns.TypeAAAA {
		return nil // Type not applicable
	}
	s.mu.RLock()
	ipAddrs, ok := s.hosts.Get(nonFqdn(r.Name))
	s.mu.RUnlock()
	if !ok {
		return nil // No match
	}
	switch s.Config.DNS.hijackMode {
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
		var ipv4Addr []net.IP
		var ipv6Addr []net.IP
		for _, ipAddr := range ipAddrs {
			if ipAddr.IP.To4() == nil {
				ipv6Addr = append(ipv6Addr, ipAddr.IP)
			} else {
				ipv4Addr = append(ipv4Addr, ipAddr.IP)
			}
		}
		switch r.Type {
		case dns.TypeA:
			return dns.ReplyA(r.Name, ipv4Addr...)
		case dns.TypeAAAA:
			return dns.ReplyAAAA(r.Name, ipv6Addr...)
		}
	}
	return nil
}

// ListenAndServe starts a server on configured address and protocol.
func (s *Server) ListenAndServe() error {
	log.Printf("dns server listening on %s [%s]", s.Config.DNS.Listen, s.Config.DNS.Protocol)
	return s.proxy.ListenAndServe(s.Config.DNS.Listen, s.Config.DNS.Protocol)
}
