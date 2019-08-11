package dns

import (
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/cache"
)

const (
	// TypeA represents the resource record type A, an IPv4 address.
	TypeA = dns.TypeA
	// TypeAAAA represents the resource record type AAAA, an IPv6 address.
	TypeAAAA = dns.TypeAAAA
	// LogDiscard disables logging of DNS requests
	LogDiscard = iota
	// LogAll logs all DNS requests
	LogAll
	// LogHijacked only logs hijacked DNS requets
	LogHijacked
)

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
	cache     *cache.Cache
	logger    logger
	logMode   int
	server    *dns.Server
	client    client
}

// ProxyOptions represents proxy configuration.
type ProxyOptions struct {
	Handler             Handler
	Resolvers           []string
	Logger              logger
	LogMode             int
	Network             string
	Timeout             time.Duration
	CacheSize           int
	CacheExpiryInterval time.Duration
}

type client interface {
	Exchange(*dns.Msg, string) (*dns.Msg, time.Duration, error)
}

type logger interface {
	Printf(string, ...interface{})
	Record(net.IP, uint16, string, string)
	Close() error
}

// NewProxy creates a new DNS proxy.
func NewProxy(options ProxyOptions) (*Proxy, error) {
	cache, err := cache.New(options.CacheSize, options.CacheExpiryInterval)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		handler:   options.Handler,
		resolvers: options.Resolvers,
		logger:    options.Logger,
		logMode:   options.LogMode,
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

// Close closes the proxy.
func (p *Proxy) Close() error {
	if p.server != nil {
		if err := p.server.Shutdown(); err != nil {
			return err
		}
	}
	return p.cache.Close()
}

func (p *Proxy) writeMsg(w dns.ResponseWriter, msg *dns.Msg, hijacked bool) {
	if p.logMode == LogAll || (hijacked && p.logMode == LogHijacked) {
		answer := ""
		if len(msg.Answer) > 0 {
			answer = msg.Answer[0].Header().Name
		}
		remoteAddr := net.ParseIP(w.RemoteAddr().String())
		p.logger.Record(remoteAddr, msg.Question[0].Qtype, msg.Question[0].Name, answer)
	}
	_ = w.WriteMsg(msg)
}

// ServeDNS implements the dns.Handler interface.
func (p *Proxy) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	if reply := p.reply(r); reply != nil {
		p.writeMsg(w, reply, true)
		return
	}
	q := r.Question[0]
	key := cache.NewKey(q.Name, q.Qtype, q.Qclass)
	if msg, ok := p.cache.Get(key); ok {
		msg.SetReply(r)
		p.writeMsg(w, msg, false)
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
		p.cache.Set(key, rr)
		p.writeMsg(w, rr, false)
		return
	}
	dns.HandleFailed(w, r)
}

// ListenAndServe listens on the network address addr and uses the server to process requests.
func (p *Proxy) ListenAndServe(addr string, network string) error {
	p.server = &dns.Server{Addr: addr, Net: network, Handler: p}
	return p.server.ListenAndServe()
}
