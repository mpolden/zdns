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

type testExchanger struct {
	mu        sync.RWMutex
	responses map[string]*response
}

func newTestExchanger() *testExchanger { return &testExchanger{responses: make(map[string]*response)} }

func (e *testExchanger) setResponse(addr string, r *response) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.responses[addr] = r
}

func (e *testExchanger) Exchange(msg *dns.Msg, addr string) (*dns.Msg, time.Duration, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, ok := e.responses[addr]
	if !ok {
		panic("no such resolver: " + addr)
	}
	if r.fail {
		return nil, 0, errors.New("error")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.answer, time.Second, nil
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
	addresses := []string{"addr1", "addr2"}
	exchanger := newTestExchanger()

	// First responding resolver returns answer
	answer1 := newA("example.com.", 60, "192.0.2.1")
	answer2 := newA("example.com.", 60, "192.0.2.2")
	r1 := response{answer: answer1}
	r1.mu.Lock() // Locking first resolver so that second wins
	exchanger.setResponse(addresses[0], &r1)
	exchanger.setResponse(addresses[1], &response{answer: answer2})
	r, err := multiExchange(exchanger, &dns.Msg{}, addresses...)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Answer[0].(*dns.A), answer2.Answer[0].(*dns.A); got != want {
		t.Errorf("got Answer[0] = %s, want %s", got, want)
	}
	r1.mu.Unlock()

	// All resolvers fail
	exchanger.setResponse(addresses[0], &response{fail: true})
	exchanger.setResponse(addresses[1], &response{fail: true})
	_, err = multiExchange(exchanger, &dns.Msg{}, addresses...)
	if err == nil {
		t.Errorf("got %s, want error", err)
	}
}
