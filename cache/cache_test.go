package cache

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func handleErr(t *testing.T, fn func() error) {
	if err := fn(); err != nil {
		t.Fatal(err)
	}
}

func newA(name string, ttl uint32, ipAddr ...net.IP) *dns.Msg {
	m := dns.Msg{}
	m.Id = dns.Id()
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	rr := make([]dns.RR, 0, len(ipAddr))
	for _, ip := range ipAddr {
		rr = append(rr, &dns.A{
			A:   ip,
			Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
		})
	}
	m.Answer = rr
	return &m
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestNewKey(t *testing.T) {
	var tests = []struct {
		name          string
		qtype, qclass uint16
		out           uint32
	}{
		{"foo.", dns.TypeA, dns.ClassINET, 3170238979},
		{"foo.", dns.TypeAAAA, dns.ClassINET, 2108186350},
		{"foo.", dns.TypeA, dns.ClassANY, 2025815293},
		{"bar.", dns.TypeA, dns.ClassINET, 1620283204},
	}
	for i, tt := range tests {
		got := NewKey(tt.name, tt.qtype, tt.qclass)
		if got != tt.out {
			t.Errorf("#%d: NewKey(%q, %d, %d) = %d, want %d", i, tt.name, tt.qtype, tt.qclass, got, tt.out)
		}
	}
}

func awaitExpiry(t *testing.T, c *Cache, k uint32) {
	now := time.Now()
	for {
		if _, ok := c.entries[k]; !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
		if time.Since(now) > 2*time.Second {
			t.Fatalf("timed out waiting for expiry of key %d", k)
		}
	}
}

func TestCache(t *testing.T) {
	m := newA("foo.", 60, net.ParseIP("192.0.2.1"))
	tt := date(2019, 1, 1)
	c, err := New(100, time.Duration(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer handleErr(t, c.Close)
	var tests = []struct {
		msg                  *dns.Msg
		createdAt, queriedAt time.Time
		ok                   bool
	}{
		{m, tt, tt, true},                        // Not expired when query time == create time
		{m, tt, tt.Add(30 * time.Second), true},  // Not expired when below TTL
		{m, tt, tt.Add(60 * time.Second), true},  // Not expired until TTL exceeds
		{m, tt, tt.Add(61 * time.Second), false}, // Expired
	}
	for i, tt := range tests {
		c.now = func() time.Time { return tt.createdAt }
		k := NewKey(tt.msg.Question[0].Name, tt.msg.Question[0].Qtype, tt.msg.Question[0].Qclass)
		c.Set(k, tt.msg)
		c.now = func() time.Time { return tt.queriedAt }
		msg, ok := c.Get(k)
		if ok != tt.ok {
			t.Errorf("#%d: Get(%d) = (%+v, %t), want (_, %t)", i, k, msg, ok, tt.ok)
		}
		if !tt.ok {
			awaitExpiry(t, c, k)
		}
		if _, ok := c.entries[k]; ok != tt.ok {
			t.Errorf("#%d: Cache[%d] = %t, want %t", i, k, ok, tt.ok)
		}
	}
}

func TestCacheMaxSize(t *testing.T) {
	var tests = []struct {
		addCount, maxSize, size int
	}{
		{1, 0, 0},
		{1, 2, 1},
		{2, 2, 2},
		{3, 2, 2},
	}
	for i, tt := range tests {
		c, err := New(tt.maxSize, 10*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		defer handleErr(t, c.Close)
		var msgs []*dns.Msg
		for i := 0; i < tt.addCount; i++ {
			m := newA(fmt.Sprintf("r%d", i), 60, net.ParseIP(fmt.Sprintf("192.0.2.%d", i)))
			k := NewKey(m.Question[0].Name, m.Question[0].Qtype, m.Question[0].Qclass)
			msgs = append(msgs, m)
			c.Set(k, m)
		}
		if got := len(c.entries); got != tt.size {
			t.Errorf("#%d: len(entries) = %d, want %d", i, got, tt.size)
		}
		if tt.maxSize > 0 && tt.addCount > tt.maxSize && tt.maxSize == tt.size {
			lastAdded := msgs[tt.addCount-1].Question[0]
			lastK := NewKey(lastAdded.Name, lastAdded.Qtype, lastAdded.Qclass)
			if _, ok := c.Get(lastK); !ok {
				t.Errorf("#%d: Get(NewKey(%q, _, _)) = (_, %t), want (_, %t)", i, lastAdded.Name, ok, !ok)
			}
			firstAdded := msgs[0].Question[0]
			firstK := NewKey(firstAdded.Name, firstAdded.Qtype, firstAdded.Qclass)
			if _, ok := c.Get(firstK); ok {
				t.Errorf("#%d: Get(NewKey(%q, _, _)) = (_, %t), want (_, %t)", i, firstAdded.Name, ok, !ok)
			}
		}
	}
}

func BenchmarkNewKey(b *testing.B) {
	for n := 0; n < b.N; n++ {
		_ = NewKey("key", 1, 1)
	}
}

func BenchmarkCache(b *testing.B) {
	c, err := New(1000, 10*time.Minute)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c.Set(uint32(n), &dns.Msg{})
		_, _ = c.Get(uint32(n))
	}
}
