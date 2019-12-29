package dns

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/cache"
	"github.com/mpolden/zdns/dns/http"
	"github.com/mpolden/zdns/dnsutil"
)

const (
	// TypeA represents th resource record type A, an IPv4 address.
	TypeA = dns.TypeA
	// TypeAAAA represents the resource record type AAAA, an IPv6 address.
	TypeAAAA = dns.TypeAAAA
	// LogDiscard disables logging of DNS requests.
	LogDiscard = iota
	// LogAll logs all DNS requests.
	LogAll
	// LogHijacked only logs hijacked DNS requests.
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
	Handler   Handler
	resolvers []string
	cache     *cache.Cache
	logger    logger
	logMode   int
	server    *dns.Server
	client    client
}

// ProxyOptions represents proxy configuration.
type ProxyOptions struct {
	Resolvers []string
	LogMode   int
	Network   string
	Timeout   time.Duration
}

type client interface {
	Exchange(*dns.Msg, string) (*dns.Msg, time.Duration, error)
}

type logger interface {
	Printf(string, ...interface{})
	Record(net.IP, bool, uint16, string, ...string)
	Close() error
}

// NewProxy creates a new DNS proxy.
func NewProxy(cache *cache.Cache, logger logger, options ProxyOptions) (*Proxy, error) {
	var c client
	if options.Network == "https" {
		c = http.NewClient(options.Timeout)
	} else {
		c = &dns.Client{Net: options.Network, Timeout: options.Timeout}
	}
	return &Proxy{
		logger:    logger,
		cache:     cache,
		resolvers: options.Resolvers,
		logMode:   options.LogMode,
		client:    c,
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
	if p.Handler == nil || len(r.Question) != 1 {
		return nil
	}
	reply := p.Handler(&Request{
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
		return p.server.Shutdown()
	}
	return nil
}

func (p *Proxy) writeMsg(w dns.ResponseWriter, msg *dns.Msg, hijacked bool) {
	if p.logMode == LogAll || (hijacked && p.logMode == LogHijacked) {
		var ip net.IP
		switch v := w.RemoteAddr().(type) {
		case *net.UDPAddr:
			ip = v.IP
		case *net.TCPAddr:
			ip = v.IP
		default:
			panic(fmt.Sprintf("unexpected remote address type %T", v))
		}
		p.logger.Record(ip, hijacked, msg.Question[0].Qtype, msg.Question[0].Name, dnsutil.Answers(msg)...)
	}
	w.WriteMsg(msg)
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
			p.logger.Printf("resolver %s failed: %s", resolver, err)
			if i == len(p.resolvers)-1 { // No more resolvers to try
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
