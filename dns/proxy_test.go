package dns

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"reflect"
	"sync"
	"testing"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/cache"
)

func init() {
	log.SetOutput(ioutil.Discard)
}

type dnsWriter struct{ lastReply *dns.Msg }

func (w *dnsWriter) LocalAddr() net.Addr { return nil }
func (w *dnsWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(192, 0, 2, 100), Port: 50000}
}
func (w *dnsWriter) Write(b []byte) (int, error) { return 0, nil }
func (w *dnsWriter) Close() error                { return nil }
func (w *dnsWriter) TsigStatus() error           { return nil }
func (w *dnsWriter) TsigTimersOnly(b bool)       {}
func (w *dnsWriter) Hijack()                     {}

func (w *dnsWriter) WriteMsg(msg *dns.Msg) error {
	w.lastReply = msg
	return nil
}

type response struct {
	answer *dns.Msg
	fail   bool
}

type testResolver struct {
	mu       sync.RWMutex
	response *response
}

func (e *testResolver) setResponse(response *response) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.response = response
}

func (e *testResolver) Exchange(msg *dns.Msg) (*dns.Msg, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r := e.response
	if r == nil || r.fail {
		return nil, fmt.Errorf("SERVFAIL")
	}
	return r.answer, nil
}

func testProxy(t *testing.T) *Proxy {
	proxy, err := NewProxy(cache.New(0, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return proxy
}

func assertRR(t *testing.T, p *Proxy, msg *dns.Msg, answer string) {
	var (
		qtype = msg.Question[0].Qtype
		qname = msg.Question[0].Name
	)
	w := &dnsWriter{}
	p.ServeDNS(w, msg)

	qtypeString := dns.TypeToString[qtype]
	answers := w.lastReply.Answer
	if got, want := len(answers), 1; got != want {
		t.Fatalf("len(msg.Answer) = %d, want %d for %s %s", got, want, qtypeString, qname)
	}
	ans := answers[0]

	if got := w.lastReply.Id; got != msg.Id {
		t.Errorf("id = %d, want %d for %s %s", got, msg.Id, qtypeString, qname)
	}

	want := net.ParseIP(answer)
	var got net.IP
	switch qtype {
	case dns.TypeA:
		rr, ok := ans.(*dns.A)
		if !ok {
			t.Errorf("type = %q, want %q for %s %s", dns.TypeToString[dns.TypeA], dns.TypeToString[rr.Header().Rrtype], qtypeString, qname)
		}
		got = rr.A
	case dns.TypeAAAA:
		rr, ok := ans.(*dns.AAAA)
		if !ok {
			t.Errorf("type = %q, want %q for %s %s", dns.TypeToString[dns.TypeA], dns.TypeToString[rr.Header().Rrtype], qtypeString, qname)
		}
		got = rr.AAAA
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IP = %s, want %s", got, want)
	}
}

func assertFailure(t *testing.T, p *Proxy, rtype uint16, qname string) {
	m := dns.Msg{}
	m.Id = dns.Id()
	m.RecursionDesired = true
	m.SetQuestion(dns.Fqdn(qname), rtype)

	w := &dnsWriter{}
	p.ServeDNS(w, &m)

	if got, want := len(w.lastReply.Answer), 0; got != want {
		t.Errorf("len(msg.Answer) = %d, want %d for %s %s", got, want, dns.TypeToString[rtype], qname)
	}
	if got, want := w.lastReply.MsgHdr.Rcode, dns.RcodeServerFailure; got != want {
		t.Errorf("MsgHdr.Rcode = %s, want %s for %s %s", dns.RcodeToString[got], dns.RcodeToString[want], dns.TypeToString[rtype], qname)
	}
}

func TestProxy(t *testing.T) {
	var h Handler = func(r *Request) *Reply {
		switch r.Type {
		case TypeA:
			return ReplyA(r.Name, net.IPv4zero)
		case TypeAAAA:
			return ReplyAAAA(r.Name, net.IPv6zero)
		}
		return nil
	}
	p := testProxy(t)
	p.Handler = h
	defer p.Close()

	m := dns.Msg{}
	m.Id = dns.Id()
	m.RecursionDesired = true

	m.SetQuestion(dns.Fqdn("badhost1"), dns.TypeA)
	assertRR(t, p, &m, "0.0.0.0")

	m.SetQuestion(dns.Fqdn("badhost1"), dns.TypeAAAA)
	assertRR(t, p, &m, "::")
}

func TestProxyWithResolver(t *testing.T) {
	p := testProxy(t)
	r := &testResolver{}
	p.client = r
	defer p.Close()
	// No response
	assertFailure(t, p, TypeA, "host1")

	// Responds succesfully
	reply := ReplyA("host1", net.ParseIP("192.0.2.1"))
	m := dns.Msg{}
	m.Id = dns.Id()
	m.SetQuestion("host1.", dns.TypeA)
	m.Answer = reply.rr
	response1 := &response{answer: &m}
	r.setResponse(response1)
	assertRR(t, p, &m, "192.0.2.1")

	// Resolver fails
	response1.fail = true
	assertFailure(t, p, TypeA, "host1")
}

func TestProxyWithCache(t *testing.T) {
	p := testProxy(t)
	p.cache = cache.New(10, nil)
	r := &testResolver{}
	p.client = r
	defer p.Close()

	reply := ReplyA("host1", net.ParseIP("192.0.2.1"))
	m := dns.Msg{}
	m.Id = dns.Id()
	m.SetQuestion("host1.", dns.TypeA)
	m.Answer = reply.rr
	r.setResponse(&response{answer: &m})
	assertRR(t, p, &m, "192.0.2.1")

	k := cache.NewKey("host1.", dns.TypeA, dns.ClassINET)
	got, ok := p.cache.Get(k)
	if !ok {
		t.Errorf("cache.Get(%d) = (%+v, %t), want (%+v, %t)", k, got, ok, m, !ok)
	}
}

func TestReplyString(t *testing.T) {
	var tests = []struct {
		fn      func(string, ...net.IP) *Reply
		fnName  string
		name    string
		ipAddrs []net.IP
		out     string
	}{
		{ReplyA, "ReplyA", "test-host", []net.IP{net.ParseIP("192.0.2.1")},
			"test-host\t3600\tIN\tA\t192.0.2.1"},
		{ReplyA, "ReplyA", "test-host", []net.IP{net.ParseIP("192.0.2.1"), net.ParseIP("192.0.2.2")},
			"test-host\t3600\tIN\tA\t192.0.2.1\ntest-host\t3600\tIN\tA\t192.0.2.2"},
		{ReplyAAAA, "ReplyAAAA", "test-host", []net.IP{net.ParseIP("2001:db8::1")},
			"test-host\t3600\tIN\tAAAA\t2001:db8::1"},
		{ReplyAAAA, "ReplyAAAA", "test-host", []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2")},
			"test-host\t3600\tIN\tAAAA\t2001:db8::1\ntest-host\t3600\tIN\tAAAA\t2001:db8::2"},
	}
	for i, tt := range tests {
		got := tt.fn(tt.name, tt.ipAddrs...).String()
		if got != tt.out {
			t.Errorf("#%d: %s(%q, %v) = %q, want %q", i, tt.fnName, tt.name, tt.ipAddrs, got, tt.out)
		}
	}
}
