package dnsutil

import (
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/dns/http"
)

var (
	// TypeToString contains a mapping of DNS request type to string.
	TypeToString = dns.TypeToString

	// RcodeToString contains a mapping of Mapping DNS response code to string.
	RcodeToString = dns.RcodeToString
)

// Client is the interface of a DNS client.
type Client interface {
	Exchange(*dns.Msg) (*dns.Msg, error)
}

// Config is a structure used to configure a DNS client.
type Config struct {
	Network string
	Timeout time.Duration
}

type resolver interface {
	Exchange(*dns.Msg, string) (*dns.Msg, time.Duration, error)
}

type client struct {
	resolver resolver
	address  string
}

type mux struct{ clients []Client }

// NewMux creates a new multiplexed client which queries all clients in parallel and returns the first successful
// response.
func NewMux(client ...Client) Client { return &mux{clients: client} }

func (m *mux) Exchange(msg *dns.Msg) (*dns.Msg, error) {
	if len(m.clients) == 0 {
		return nil, fmt.Errorf("no clients to query")
	}
	responses := make(chan *dns.Msg, len(m.clients))
	errs := make(chan error, len(m.clients))
	var wg sync.WaitGroup
	for _, c := range m.clients {
		wg.Add(1)
		go func(client Client) {
			defer wg.Done()
			r, err := client.Exchange(msg)
			if err != nil {
				errs <- err
				return
			}
			responses <- r
		}(c)
	}
	go func() {
		wg.Wait()
		close(errs)
		close(responses)
	}()
	for rr := range responses {
		return rr, nil
	}
	return nil, <-errs
}

// NewClient creates a new Client for addr using config.
func NewClient(addr string, config Config) Client {
	var r resolver
	if config.Network == "https" {
		r = http.NewClient(config.Timeout)
	} else {
		var tlsConfig *tls.Config
		parts := strings.SplitN(addr, "=", 2)
		if len(parts) == 2 {
			addr = parts[0]
			tlsConfig = &tls.Config{ServerName: parts[1]}
		}
		r = &dns.Client{Net: config.Network, Timeout: config.Timeout, TLSConfig: tlsConfig}
	}
	return &client{resolver: r, address: addr}
}

func (c *client) Exchange(msg *dns.Msg) (*dns.Msg, error) {
	r, _, err := c.resolver.Exchange(msg, c.address)
	if err != nil {
		return nil, fmt.Errorf("resolver %s failed: %w", c.address, err)
	}
	return r, err
}

// Answers returns all values in the answer section of DNS message msg.
func Answers(msg *dns.Msg) []string {
	var answers []string
	for _, answer := range msg.Answer {
		for i := 1; i <= dns.NumField(answer); i++ {
			answers = append(answers, dns.Field(answer, i))
		}
	}
	return answers
}

// MinTTL returns the lowest TTL of of answer, authority and additional sections.
func MinTTL(msg *dns.Msg) time.Duration {
	var ttl uint32 = (1 << 31) - 1 // Maximum TTL from RFC 2181
	for _, answer := range msg.Answer {
		ttl = min(answer.Header().Ttl, ttl)
	}
	for _, ns := range msg.Ns {
		ttl = min(ns.Header().Ttl, ttl)
	}
	for _, extra := range msg.Extra {
		// OPT (EDNS) is a pseudo record which uses TTL field for extended RCODE and flags
		if extra.Header().Rrtype == dns.TypeOPT {
			continue
		}
		ttl = min(extra.Header().Ttl, ttl)
	}
	return time.Duration(ttl) * time.Second
}

func min(x, y uint32) uint32 {
	if x < y {
		return x
	}
	return y
}
