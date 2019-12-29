package dns

import (
	"fmt"
	"io/ioutil"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/cache"
	"github.com/mpolden/zdns/log"
)

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

type resolver struct {
	answer *dns.Msg
	fail   bool
}

type testClient map[string]*resolver

func (c testClient) Exchange(msg *dns.Msg, addr string) (*dns.Msg, time.Duration, error) {
	r, ok := c[addr]
	if !ok {
		panic("no such resolver: " + addr)
	}
	if r.fail {
		return nil, 0, fmt.Errorf("%s SERVFAIL", addr)
	}
	return r.answer, time.Minute * 5, nil
}

type testLogger struct {
	question   string
	remoteAddr net.IP
}

func (l *testLogger) Close() error                           { return nil }
func (l *testLogger) Printf(format string, v ...interface{}) {}
func (l *testLogger) Record(remoteAddr net.IP, hijacked bool, qtype uint16, question string, answers ...string) {
	l.question = question
	l.remoteAddr = remoteAddr
}

func testProxy(t *testing.T) *Proxy {
	log, err := log.New(ioutil.Discard, "", log.RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewProxy(cache.New(0), log, ProxyOptions{})
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

func TestProxyWithResolvers(t *testing.T) {
	p := testProxy(t)
	p.resolvers = []string{"resolver1"}
	client := make(testClient)
	p.client = client
	defer p.Close()

	// First and only resolver responds succesfully
	reply := ReplyA("host1", net.ParseIP("192.0.2.1"))
	m := dns.Msg{}
	m.Id = dns.Id()
	m.SetQuestion("host1.", dns.TypeA)
	m.Answer = reply.rr
	client["resolver1"] = &resolver{answer: &m}
	assertRR(t, p, &m, "192.0.2.1")

	// First and only resolver fails
	client["resolver1"].fail = true
	assertFailure(t, p, TypeA, "host1")

	// First resolver fails, but second succeeds
	reply = ReplyA("host1", net.ParseIP("192.0.2.2"))
	p.resolvers = []string{"resolver1", "resolver2"}
	m = dns.Msg{}
	m.Id = dns.Id()
	m.SetQuestion("host1.", dns.TypeA)
	m.Answer = reply.rr
	client["resolver2"] = &resolver{answer: &m}
	assertRR(t, p, &m, "192.0.2.2")

	// All resolvers fail
	client["resolver2"].fail = true
	assertFailure(t, p, TypeA, "host1")
}

func TestProxyWithCache(t *testing.T) {
	p := testProxy(t)
	p.cache = cache.New(10)
	p.resolvers = []string{"resolver1"}
	client := make(testClient)
	p.client = client
	defer p.Close()

	reply := ReplyA("host1", net.ParseIP("192.0.2.1"))
	m := dns.Msg{}
	m.Id = dns.Id()
	m.SetQuestion("host1.", dns.TypeA)
	m.Answer = reply.rr
	client["resolver1"] = &resolver{answer: &m}
	assertRR(t, p, &m, "192.0.2.1")

	k := cache.NewKey("host1.", dns.TypeA, dns.ClassINET)
	got, ok := p.cache.Get(k)
	if !ok {
		t.Errorf("cache.Get(%d) = (%+v, %t), want (%+v, %t)", k, got, ok, m, !ok)
	}
}

func TestProxyWithLogging(t *testing.T) {
	logger := &testLogger{}
	p, err := NewProxy(cache.New(0), logger, ProxyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	badHost := "badhost1."
	goodHost := "goodhost1."
	p.resolvers = []string{"resolver1"}
	client := make(testClient)
	p.client = client
	reply := ReplyA(goodHost, net.ParseIP("192.0.2.1"))
	m := dns.Msg{}
	m.Id = dns.Id()
	m.RecursionDesired = true
	m.Answer = reply.rr
	client["resolver1"] = &resolver{answer: &m}
	var h Handler = func(r *Request) *Reply {
		if r.Name == badHost {
			return ReplyA(r.Name, net.IPv4zero)
		}
		return nil
	}
	p.Handler = h

	var tests = []struct {
		question   string
		remoteAddr net.IP
		log        bool
		logMode    int
	}{
		{badHost, net.IPv4(192, 0, 2, 100), true, LogAll},
		{goodHost, net.IPv4(192, 0, 2, 100), true, LogAll},
		{badHost, net.IPv4(192, 0, 2, 100), true, LogHijacked},
		{goodHost, net.IPv4(192, 0, 2, 100), false, LogHijacked},
		{badHost, net.IPv4(192, 0, 2, 100), false, LogDiscard},
		{goodHost, net.IPv4(192, 0, 2, 100), false, LogDiscard},
	}
	for i, tt := range tests {
		logger.question = ""
		logger.remoteAddr = nil
		p.logMode = tt.logMode
		m.SetQuestion(tt.question, dns.TypeA)
		if tt.question == badHost {
			assertRR(t, p, &m, "0.0.0.0")
		} else {
			assertRR(t, p, &m, "192.0.2.1")
		}
		if tt.log {
			if logger.question != tt.question {
				t.Errorf("#%d: question = %q, want %q", i, logger.question, tt.question)
			}
			if logger.remoteAddr.String() != tt.remoteAddr.String() {
				t.Errorf("#%d: remoteAddr = %s, want %s", i, logger.remoteAddr, tt.remoteAddr)
			}
		} else {
			if logger.question != "" {
				t.Errorf("#%d: question = %q, want %q", i, logger.question, "")
			}
			if logger.remoteAddr != nil {
				t.Errorf("#%d: remoteAddr = %v, want %v", i, logger.remoteAddr, nil)
			}
		}
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
