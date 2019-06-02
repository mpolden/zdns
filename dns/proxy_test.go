package dns

import (
	"net"
	"reflect"
	"testing"

	"github.com/miekg/dns"
)

type dnsWriter struct{ lastReply *dns.Msg }

func (w *dnsWriter) LocalAddr() net.Addr         { return nil }
func (w *dnsWriter) RemoteAddr() net.Addr        { return nil }
func (w *dnsWriter) Write(b []byte) (int, error) { return 0, nil }
func (w *dnsWriter) Close() error                { return nil }
func (w *dnsWriter) TsigStatus() error           { return nil }
func (w *dnsWriter) TsigTimersOnly(b bool)       {}
func (w *dnsWriter) Hijack()                     {}

func (w *dnsWriter) WriteMsg(m *dns.Msg) error {
	w.lastReply = m
	return nil
}

func assertRR(t *testing.T, p *Proxy, rtype uint16, qname, answer string) {
	m := dns.Msg{}
	m.Id = dns.Id()
	m.RecursionDesired = true
	m.SetQuestion(dns.Fqdn(qname), rtype)

	w := &dnsWriter{}
	p.ServeDNS(w, &m)

	answers := w.lastReply.Answer
	if len(answers) != 1 {
		t.Fatalf("want 1 answer, got %d", len(answers))
	}
	a := answers[0]

	want := net.ParseIP(answer)
	var got net.IP
	switch rtype {
	case dns.TypeA:
		rr, ok := a.(*dns.A)
		if !ok {
			t.Errorf("want type = %s, got %s", dns.TypeToString[dns.TypeA], dns.TypeToString[rr.Header().Rrtype])
		}
		got = rr.A
	case dns.TypeAAAA:
		rr, ok := a.(*dns.AAAA)
		if !ok {
			t.Errorf("want type = %s, got %s", dns.TypeToString[dns.TypeA], dns.TypeToString[rr.Header().Rrtype])
		}
		got = rr.AAAA
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %s, got %s", want, got)
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
	p := NewProxy(h, nil, 0)
	assertRR(t, p, TypeA, "badhost1", "0.0.0.0")
	assertRR(t, p, TypeAAAA, "badhost1", "::")
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
