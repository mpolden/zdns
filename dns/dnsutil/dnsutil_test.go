package dnsutil

import (
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type response struct {
	answer *dns.Msg
	fail   bool
	mu     sync.Mutex
}

type testResolver struct {
	mu       sync.RWMutex
	response *response
}

func (e *testResolver) setResponse(r *response) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.response = r
}

func (e *testResolver) Exchange(msg *dns.Msg) (*dns.Msg, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r := e.response
	if r == nil {
		panic("no response set")
	}
	if r.fail {
		return nil, errors.New("error")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.answer, nil
}

func newA(name string, ttl uint32, ipAddr ...string) *dns.Msg {
	m := dns.Msg{}
	m.Id = dns.Id()
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	rr := make([]dns.RR, 0, len(ipAddr))
	for _, ip := range ipAddr {
		rr = append(rr, &dns.A{
			A:   net.ParseIP(ip),
			Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
		})
	}
	m.Answer = rr
	return &m
}

func TestMinTTL(t *testing.T) {
	var tests = []struct {
		answer []dns.RR
		ns     []dns.RR
		extra  []dns.RR
		ttl    time.Duration
	}{
		{
			[]dns.RR{
				&dns.A{Hdr: dns.RR_Header{Ttl: 3600}},
				&dns.A{Hdr: dns.RR_Header{Ttl: 60}},
			},
			nil,
			nil,
			time.Minute,
		},
		{
			[]dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 60}}},
			[]dns.RR{&dns.NS{Hdr: dns.RR_Header{Ttl: 30}}},
			nil,
			30 * time.Second,
		},
		{
			[]dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 60}}},
			[]dns.RR{&dns.NS{Hdr: dns.RR_Header{Ttl: 30}}},
			[]dns.RR{&dns.NS{Hdr: dns.RR_Header{Ttl: 10}}},
			10 * time.Second,
		},
		{
			[]dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 60}}},
			nil,
			[]dns.RR{
				&dns.OPT{Hdr: dns.RR_Header{Ttl: 10, Rrtype: dns.TypeOPT}}, // Ignored
				&dns.A{Hdr: dns.RR_Header{Ttl: 30}},
			},
			30 * time.Second,
		},
	}
	for i, tt := range tests {
		msg := dns.Msg{}
		msg.Answer = tt.answer
		msg.Ns = tt.ns
		msg.Extra = tt.extra
		if got := MinTTL(&msg); got != tt.ttl {
			t.Errorf("#%d: MinTTL(\n%s) = %s, want %s", i, msg.String(), got, tt.ttl)
		}
	}
}

func TestAnswers(t *testing.T) {
	var tests = []struct {
		rr  []dns.RR
		out []string
	}{
		{[]dns.RR{&dns.A{A: net.ParseIP("192.0.2.1")}}, []string{"192.0.2.1"}},
		{[]dns.RR{
			&dns.A{A: net.ParseIP("192.0.2.1")},
			&dns.A{A: net.ParseIP("192.0.2.2")},
		}, []string{"192.0.2.1", "192.0.2.2"}},
		{[]dns.RR{&dns.AAAA{AAAA: net.ParseIP("2001:db8::1")}}, []string{"2001:db8::1"}},
	}
	for i, tt := range tests {
		msg := dns.Msg{Answer: tt.rr}
		if got, want := Answers(&msg), tt.out; !reflect.DeepEqual(got, want) {
			t.Errorf("#%d: Answers(%+v) = %+v, want %+v", i, tt.rr, got, want)
		}
	}
}

func TestExchange(t *testing.T) {
	resolver1 := &testResolver{}
	resolver2 := &testResolver{}

	// First responding resolver returns answer
	answer1 := newA("example.com.", 60, "192.0.2.1")
	answer2 := newA("example.com.", 60, "192.0.2.2")
	r1 := response{answer: answer1}
	r1.mu.Lock() // Locking first resolver so that second wins
	resolver1.setResponse(&r1)
	resolver2.setResponse(&response{answer: answer2})

	mux := NewMux(resolver1, resolver2)
	r, err := mux.Exchange(&dns.Msg{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Answer[0].(*dns.A), answer2.Answer[0].(*dns.A); got != want {
		t.Errorf("got Answer[0] = %s, want %s", got, want)
	}
	r1.mu.Unlock()

	// All resolvers fail
	resolver1.setResponse(&response{fail: true})
	resolver2.setResponse(&response{fail: true})
	_, err = mux.Exchange(&dns.Msg{})
	if err == nil {
		t.Errorf("got %s, want error", err)
	}
}
