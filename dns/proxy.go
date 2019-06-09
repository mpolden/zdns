package dns

import (
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/cache"
)

// TypeA represents the resource record type A, an IPv4 address.
const TypeA = dns.TypeA

// TypeAAAA represents the resource record type AAAA, an IPv6 address.
const TypeAAAA = dns.TypeAAAA

// Request represents a simplified DNS request.
type Request struct {
	Type uint16
	Name string
}

// Reply represents a simplifed DNS reply.
type Reply struct{ rr []dns.RR }

// Handler represents the handler for a DNS request.
type Handler func(*Request) *Reply

// Proxy represents a DNS proxy.
type Proxy struct {
	handler   Handler
	resolvers []string
	now       func() time.Time
	cache     *cache.Cache
	logger    *log.Logger
	server    *dns.Server
	client    client
}

// ProxyOptions represents proxy configuration.
type ProxyOptions struct {
	Handler   Handler
	Resolvers []string
	Logger    *log.Logger
	Network   string
	Timeout   time.Duration
	CacheSize int
}

type client interface {
	Exchange(*dns.Msg, string) (*dns.Msg, time.Duration, error)
}

// NewProxy creates a new DNS proxy.
func NewProxy(options ProxyOptions) (*Proxy, error) {
	cache, err := cache.New(options.CacheSize)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		handler:   options.Handler,
		resolvers: options.Resolvers,
		now:       time.Now,
		logger:    options.Logger,
		cache:     cache,
		client:    &dns.Client{Net: options.Network, Timeout: options.Timeout},
	}, nil
}

// ReplyA creates a resource record of type A.
func ReplyA(name string, ipAddr ...net.IP) *Reply {
	rr := make([]dns.RR, 0, len(ipAddr))
	for _, ip := range ipAddr {
		rr = append(rr, &dns.A{
			A:   ip,
			Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
		})
	}
	return &Reply{rr}
}

// ReplyAAAA creates a resource record of type AAAA.
func ReplyAAAA(name string, ipAddr ...net.IP) *Reply {
	rr := make([]dns.RR, 0, len(ipAddr))
	for _, ip := range ipAddr {
		rr = append(rr, &dns.AAAA{
			AAAA: ip,
			Hdr:  dns.RR_Header{Name: name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 3600},
		})
	}
	return &Reply{rr}
}

func (r *Reply) String() string {
	b := strings.Builder{}
	for i, rr := range r.rr {
		b.WriteString(rr.String())
		if i < len(r.rr)-1 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}

func (p *Proxy) reply(r *dns.Msg) *dns.Msg {
	if p.handler == nil || len(r.Question) != 1 {
		return nil
	}
	reply := p.handler(&Request{
		Name: r.Question[0].Name,
		Type: r.Question[0].Qtype,
	})
	if reply == nil {
		return nil
	}
	m := dns.Msg{Answer: reply.rr}
	// Pretend this is an recursive answer
	m.RecursionAvailable = true
	m.SetReply(r)
	return &m
}

// Close closes the proxy and release associated resources.
func (p *Proxy) Close() error {
	if p.server != nil {
		return p.server.Shutdown()
	}
	return nil
}

// ServeDNS implements the dns.Handler interface.
func (p *Proxy) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	reply := p.reply(r)
	if reply != nil {
		_ = w.WriteMsg(reply) // TODO: Decide whether to handle write errors
		return
	}
	q := r.Question[0]
	key := cache.NewKey(q.Name, q.Qtype, q.Qclass)
	if msg, ok := p.cache.Get(key, p.now()); ok {
		msg.SetReply(r)
		_ = w.WriteMsg(&msg)
		return
	}
	for i, resolver := range p.resolvers {
		rr, _, err := p.client.Exchange(r, resolver)
		if err != nil {
			if p.logger != nil {
				p.logger.Printf("resolver %s failed: %s", resolver, err)
			}
			if i == len(p.resolvers)-1 {
				break
			} else {
				continue
			}
		}
		p.cache.Add(rr, p.now())
		_ = w.WriteMsg(rr)
		return
	}
	dns.HandleFailed(w, r)
}

// ListenAndServe listens on the network address addr and uses the server to process requests.
func (p *Proxy) ListenAndServe(addr string, network string) error {
	p.server = &dns.Server{Addr: addr, Net: network, Handler: p}
	return p.server.ListenAndServe()
}
