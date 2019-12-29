package cache

import (
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/miekg/dns"
)

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

func reverse(msgs []*dns.Msg) []*dns.Msg {
	reversed := make([]*dns.Msg, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		reversed = append(reversed, msgs[i])
	}
	return reversed
}

func awaitExpiry(t *testing.T, c *Cache, k uint64) {
	now := time.Now()
	for { // Loop until k is removed by maintainer
		c.mu.RLock()
		_, ok := c.values[k]
		c.mu.RUnlock()
		if !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
		if time.Since(now) > 2*time.Second {
			t.Fatalf("timed out waiting for expiry of key %d", k)
		}
	}
}

func TestNewKey(t *testing.T) {
	var tests = []struct {
		name          string
		qtype, qclass uint16
		out           uint64
	}{
		{"foo.", dns.TypeA, dns.ClassINET, 12854986581909659251},
		{"foo.", dns.TypeAAAA, dns.ClassINET, 12509032947198407788},
		{"foo.", dns.TypeA, dns.ClassANY, 12855125120374813837},
		{"bar.", dns.TypeA, dns.ClassINET, 4069151952488606484},
	}
	for i, tt := range tests {
		got := NewKey(tt.name, tt.qtype, tt.qclass)
		if got != tt.out {
			t.Errorf("#%d: NewKey(%q, %d, %d) = %d, want %d", i, tt.name, tt.qtype, tt.qclass, got, tt.out)
		}
	}
}

func TestCache(t *testing.T) {
	msg := newA("r1.", 60, net.ParseIP("192.0.2.1"), net.ParseIP("192.0.2.2"))
	msgWithZeroTTL := newA("r2.", 0, net.ParseIP("192.0.2.2"))
	msgFailure := newA("r3.", 60, net.ParseIP("192.0.2.2"))
	msgFailure.Rcode = dns.RcodeServerFailure
	msgNameError := &dns.Msg{}
	msgNameError.Id = dns.Id()
	msgNameError.SetQuestion(dns.Fqdn("r4."), dns.TypeA)
	msgNameError.Rcode = dns.RcodeNameError

	now := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	c := newCache(100, 10*time.Millisecond, nowFn)
	defer c.Close()
	var tests = []struct {
		msg       *dns.Msg
		queriedAt time.Time
		ok        bool
		value     *Value
	}{
		{msg, now, true, &Value{CreatedAt: now, msg: msg}},                       // Not expired when query time == create time
		{msg, now.Add(30 * time.Second), true, &Value{CreatedAt: now, msg: msg}}, // Not expired when below TTL
		{msg, now.Add(60 * time.Second), true, &Value{CreatedAt: now, msg: msg}}, // Not expired until TTL exceeds
		{msgNameError, now, true, &Value{CreatedAt: now, msg: msgNameError}},     // NXDOMAIN is cached
		{msg, now.Add(61 * time.Second), false, nil},                             // Expired due to TTL exceeded
		{msgWithZeroTTL, now, false, nil},                                        // 0 TTL is not cached
		{msgFailure, now, false, nil},                                            // Non-cacheable rcode
	}
	for i, tt := range tests {
		c.now = nowFn
		k := NewKey(tt.msg.Question[0].Name, tt.msg.Question[0].Qtype, tt.msg.Question[0].Qclass)
		c.Set(k, tt.msg)
		c.now = func() time.Time { return tt.queriedAt }
		if msg, ok := c.Get(k); ok != tt.ok {
			t.Errorf("#%d: Get(%d) = (%+v, %t), want (_, %t)", i, k, msg, ok, tt.ok)
		}
		if v, ok := c.getValue(k); ok != tt.ok || !reflect.DeepEqual(v, tt.value) {
			t.Errorf("#%d: getValue(%d) = (%+v, %t), want (%+v, %t)", i, k, v, ok, tt.value, tt.ok)
		}
		if !tt.ok {
			awaitExpiry(t, c, k)
		}
		if _, ok := c.values[k]; ok != tt.ok {
			t.Errorf("#%d: values[%d] = %t, want %t", i, k, ok, tt.ok)
		}
		keyIdx := -1
		for i, key := range c.keys {
			if key == k {
				keyIdx = i
				break
			}
		}
		if (keyIdx != -1) != tt.ok {
			t.Errorf("#%d: keys[%d] = %d, found expired key", i, keyIdx, k)
		}
	}
}

func TestCacheCapacity(t *testing.T) {
	var tests = []struct {
		addCount, capacity, size int
	}{
		{1, 0, 0},
		{1, 2, 1},
		{2, 2, 2},
		{3, 2, 2},
	}
	for i, tt := range tests {
		c := New(tt.capacity)
		defer c.Close()
		var msgs []*dns.Msg
		for i := 0; i < tt.addCount; i++ {
			m := newA(fmt.Sprintf("r%d", i), 60, net.ParseIP(fmt.Sprintf("192.0.2.%d", i)))
			k := NewKey(m.Question[0].Name, m.Question[0].Qtype, m.Question[0].Qclass)
			msgs = append(msgs, m)
			c.Set(k, m)
		}
		if got := len(c.values); got != tt.size {
			t.Errorf("#%d: len(values) = %d, want %d", i, got, tt.size)
		}
		if tt.capacity > 0 && tt.addCount > tt.capacity && tt.capacity == tt.size {
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

func TestCacheList(t *testing.T) {
	var tests = []struct {
		addCount, listCount, wantCount int
		expire                         bool
	}{
		{0, 0, 0, false},
		{1, 0, 0, false},
		{1, 1, 1, false},
		{2, 1, 1, false},
		{2, 3, 2, false},
		{2, 0, 0, true},
	}
	for i, tt := range tests {
		c := New(1024)
		defer c.Close()
		var msgs []*dns.Msg
		for i := 0; i < tt.addCount; i++ {
			m := newA(fmt.Sprintf("r%d", i), 60, net.ParseIP(fmt.Sprintf("192.0.2.%d", i)))
			k := NewKey(m.Question[0].Name, m.Question[0].Qtype, m.Question[0].Qclass)
			msgs = append(msgs, m)
			c.Set(k, m)
		}
		if tt.expire {
			c.now = func() time.Time { return time.Now().Add(time.Minute).Add(time.Second) }
		}
		values := c.List(tt.listCount)
		if got := len(values); got != tt.wantCount {
			t.Errorf("#%d: len(List(%d)) = %d, want %d", i, tt.listCount, got, tt.wantCount)
		}
		gotMsgs := make([]*dns.Msg, 0, len(values))
		for _, v := range values {
			gotMsgs = append(gotMsgs, v.msg)
		}
		msgs = reverse(msgs)
		want := msgs[:tt.wantCount]
		if !reflect.DeepEqual(want, gotMsgs) {
			t.Errorf("#%d: got %+v, want %+v", i, gotMsgs, want)
		}
	}
}

func TestReset(t *testing.T) {
	c := New(10)
	c.Set(uint64(1), &dns.Msg{})
	c.Reset()
	if got, want := len(c.values), 0; got != want {
		t.Errorf("len(values) = %d, want %d", got, want)
	}
	if got, want := len(c.keys), 0; got != want {
		t.Errorf("len(keys) = %d, want %d", got, want)
	}
}

func BenchmarkNewKey(b *testing.B) {
	for n := 0; n < b.N; n++ {
		NewKey("key", 1, 1)
	}
}

func BenchmarkCache(b *testing.B) {
	c := New(1000)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c.Set(uint64(n), &dns.Msg{})
		c.Get(uint64(n))
	}
}

func BenchmarkCacheEviction(b *testing.B) {
	c := New(1)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c.Set(uint64(n), &dns.Msg{})
		c.Get(uint64(n))
	}
}
