package cache

import (
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/dns/dnsutil"
)

var testMsg *dns.Msg = newA("example.com.", 60, net.ParseIP("192.0.2.1"))

type testClient struct {
	mu      sync.RWMutex
	answers chan *dns.Msg
}

func newTestClient() *testClient { return &testClient{answers: make(chan *dns.Msg, 100)} }

func (e *testClient) setAnswer(answer *dns.Msg) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.answers <- answer
}

func (e *testClient) reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.answers = make(chan *dns.Msg, 100)
}

func (e *testClient) Exchange(msg *dns.Msg) (*dns.Msg, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.answers) == 0 {
		return nil, fmt.Errorf("no answer pending")
	}
	return <-e.answers, nil
}

type testBackend struct {
	values []Value
}

func (b *testBackend) Set(key uint32, value Value) {
	b.values = append(b.values, value)
}

func (b *testBackend) Evict(key uint32) {
	var values []Value
	for _, v := range b.values {
		if v.Key == key {
			continue
		}
		values = append(values, v)
	}
	b.values = values
}

func (b *testBackend) Reset() { b.values = nil }

func (b *testBackend) Read() []Value { return b.values }

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

func TestNewKey(t *testing.T) {
	var tests = []struct {
		name          string
		qtype, qclass uint16
		out           uint32
	}{
		{"foo.", dns.TypeA, dns.ClassINET, 2839090419},
		{"foo.", dns.TypeAAAA, dns.ClassINET, 3344654668},
		{"foo.", dns.TypeA, dns.ClassANY, 1731870733},
		{"bar.", dns.TypeA, dns.ClassINET, 1951431764},
	}
	for i, tt := range tests {
		got := NewKey(tt.name, tt.qtype, tt.qclass)
		if got != tt.out {
			t.Errorf("#%d: NewKey(%q, %d, %d) = %d, want %d", i, tt.name, tt.qtype, tt.qclass, got, tt.out)
		}
	}
}

func TestCache(t *testing.T) {
	msg := newA("1.example.com.", 60, net.ParseIP("192.0.2.1"), net.ParseIP("192.0.2.2"))
	msgWithZeroTTL := newA("2.example.com.", 0, net.ParseIP("192.0.2.2"))
	msgFailure := newA("3.example.com.", 60, net.ParseIP("192.0.2.2"))
	msgFailure.Rcode = dns.RcodeServerFailure
	msgNameError := &dns.Msg{}
	msgNameError.Id = dns.Id()
	msgNameError.SetQuestion(dns.Fqdn("r4."), dns.TypeA)
	msgNameError.Rcode = dns.RcodeNameError

	now := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(100, nil)
	var tests = []struct {
		msg       *dns.Msg
		queriedAt time.Time
		ok        bool
		value     *Value
	}{
		{msg, now, true, &Value{Key: 3517338631, CreatedAt: now, msg: msg}},                       // Not expired when query time == create time
		{msg, now.Add(30 * time.Second), true, &Value{Key: 3517338631, CreatedAt: now, msg: msg}}, // Not expired when below TTL
		{msg, now.Add(60 * time.Second), true, &Value{Key: 3517338631, CreatedAt: now, msg: msg}}, // Not expired until TTL exceeds
		{msgNameError, now, true, &Value{Key: 3980405151, CreatedAt: now, msg: msgNameError}},     // NXDOMAIN is cached
		{msg, now.Add(61 * time.Second), false, nil},                                              // Expired due to TTL exceeded
		{msgWithZeroTTL, now, false, nil},                                                         // 0 TTL is not cached
		{msgFailure, now, false, nil},                                                             // Non-cacheable rcode
	}
	for i, tt := range tests {
		c.now = func() time.Time { return now }
		k := NewKey(tt.msg.Question[0].Name, tt.msg.Question[0].Qtype, tt.msg.Question[0].Qclass)
		c.Set(k, tt.msg)
		c.now = func() time.Time { return tt.queriedAt }
		if msg, ok := c.Get(k); ok != tt.ok {
			t.Errorf("#%d: Get(%d) = (%+v, %t), want (_, %t)", i, k, msg, ok, tt.ok)
		}
		if v, ok := c.getValue(k); ok != tt.ok || !reflect.DeepEqual(v, tt.value) {
			t.Errorf("#%d: getValue(%d) = (%+v, %t), want (%+v, %t)", i, k, v, ok, tt.value, tt.ok)
		}
		c.Close()
		c.mu.RLock()
		if _, ok := c.entries[k]; ok != tt.ok {
			t.Errorf("#%d: values[%d] = %t, want %t", i, k, ok, tt.ok)
		}
		keyIdx := -1
		for el := c.values.Front(); el != nil; el = el.Next() {
			if el.Value.(Value).Key == k {
				keyIdx = i
				break
			}
		}
		c.mu.RUnlock()
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
		c := New(tt.capacity, nil)
		var msgs []*dns.Msg
		for i := 0; i < tt.addCount; i++ {
			m := newA(fmt.Sprintf("r%d", i), 60, net.ParseIP(fmt.Sprintf("192.0.2.%d", i)))
			k := NewKey(m.Question[0].Name, m.Question[0].Qtype, m.Question[0].Qclass)
			msgs = append(msgs, m)
			c.Set(k, m)
		}
		if got := len(c.entries); got != tt.size {
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
		c := New(1024, nil)
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
	c := New(10, nil)
	c.Set(uint32(1), &dns.Msg{})
	c.Reset()
	if got, want := len(c.entries), 0; got != want {
		t.Errorf("len(values) = %d, want %d", got, want)
	}
	if got, want := c.values.Len(), 0; got != want {
		t.Errorf("len(keys) = %d, want %d", got, want)
	}
}

func TestCachePrefetch(t *testing.T) {
	client := newTestClient()
	now := time.Now()
	c := newCache(10, client, nil, func() time.Time { return now })
	var tests = []struct {
		initialAnswer string
		refreshAnswer string
		initialTTL    time.Duration
		refreshTTL    time.Duration
		readDelay     time.Duration
		answer        string
		ok            bool
		refetch       bool
	}{
		// Serves cached value before expiry
		{"192.0.2.1", "192.0.2.42", time.Minute, time.Minute, 30 * time.Second, "192.0.2.1", true, true},
		// Serves stale cached value after expiry and before refresh happens
		{"192.0.2.1", "192.0.2.42", time.Minute, time.Minute, 61 * time.Second, "192.0.2.1", true, false},
		// Serves refreshed value after expiry and refresh
		{"192.0.2.1", "192.0.2.42", time.Minute, time.Minute, 61 * time.Second, "192.0.2.42", true, true},
		// Refreshed value can no longer be cached
		{"192.0.2.1", "192.0.2.42", time.Minute, 0, 61 * time.Second, "192.0.2.42", false, true},
	}
	for i, tt := range tests {
		copy := testMsg.Copy()
		copy.Answer[0].(*dns.A).A = net.ParseIP(tt.refreshAnswer)
		copy.Answer[0].(*dns.A).Hdr.Ttl = uint32(tt.refreshTTL.Seconds())
		client.reset()
		client.setAnswer(copy)

		// Add new value now
		c.now = func() time.Time { return now }
		var key uint32 = 1
		c.Set(key, testMsg)

		// Read value at some point in the future
		c.now = func() time.Time { return now.Add(tt.readDelay) }
		v, ok := c.getValue(key)
		c.Close() // Flush queued operations

		if tt.refetch {
			v, ok = c.getValue(key)
		}
		if ok != tt.ok {
			t.Errorf("#%d: Get(%d) = (_, %t), want (_, %t)", i, key, ok, tt.ok)
		}
		if tt.ok {
			answers := dnsutil.Answers(v.msg)
			if answers[0] != tt.answer {
				t.Errorf("#%d: Get(%d) = (%q, _), want (%q, _)", i, key, answers[0], tt.answer)
			}
		}
	}
}

func TestCacheEvictAndUpdate(t *testing.T) {
	client := newTestClient()
	now := time.Now()
	c := newCache(10, client, nil, func() time.Time { return now })

	var key uint32 = 1
	c.Set(key, testMsg)

	// Initial prefetched answer can no longer be cached
	copy := testMsg.Copy()
	copy.Answer[0].(*dns.A).Hdr.Ttl = 0
	client.setAnswer(copy)
	copy = testMsg.Copy()
	copy.Answer[0].(*dns.A).Hdr.Ttl = 30
	client.setAnswer(copy)

	// Advance time so that msg is now considered expired. Query to trigger prefetch
	c.now = func() time.Time { return now.Add(61 * time.Second) }
	c.Get(key)

	// Query again, causing another prefetch with a non-zero TTL
	c.Get(key)

	// Last query refreshes key
	c.Close()
	keyExists := false
	for el := c.values.Front(); el != nil; el = el.Next() {
		if el.Value.(Value).Key == key {
			keyExists = true
		}
	}
	if !keyExists {
		t.Errorf("expected cache keys to contain %d", key)
	}
}

func TestPackValue(t *testing.T) {
	v := Value{
		Key:       42,
		CreatedAt: time.Now().Truncate(time.Second),
		msg:       testMsg,
	}
	packed, err := v.Pack()
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := Unpack(packed)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := unpacked.Key, v.Key; got != want {
		t.Errorf("Key = %d, want %d", got, want)
	}
	if got, want := unpacked.CreatedAt, v.CreatedAt; !got.Equal(want) {
		t.Errorf("CreatedAt = %s, want %s", got, want)
	}
	if got, want := unpacked.msg.String(), v.msg.String(); got != want {
		t.Errorf("msg = %s, want %s", got, want)
	}
}

func TestCacheWithBackend(t *testing.T) {
	var tests = []struct {
		capacity    int
		backendSize int
		cacheSize   int
	}{
		{0, 0, 0},
		{0, 1, 0},
		{1, 0, 0},
		{1, 1, 1},
		{1, 2, 1},
		{2, 1, 1},
		{2, 2, 2},
		{3, 2, 2},
	}
	for i, tt := range tests {
		backend := &testBackend{}
		for j := 0; j < tt.backendSize; j++ {
			v := Value{
				Key:       uint32(j),
				CreatedAt: time.Now(),
				msg:       testMsg,
			}
			backend.Set(v.Key, v)
		}
		c := NewWithBackend(tt.capacity, nil, backend)
		if got, want := len(c.entries), tt.cacheSize; got != want {
			t.Errorf("#%d: len(values) = %d, want %d", i, got, want)
		}
		if tt.backendSize > tt.capacity {
			if got, want := len(backend.Read()), tt.capacity; got != want {
				t.Errorf("#%d: len(backend.Read()) = %d, want %d", i, got, want)
			}
		}
		if tt.capacity == tt.backendSize {
			// Adding a new entry to a cache at capacity removes the oldest from backend
			c.Set(42, testMsg)
			if got, want := len(backend.Read()), tt.capacity; got != want {
				t.Errorf("#%d: len(backend.Read()) = %d, want %d", i, got, want)
			}
		}
	}
}

func TestCacheStats(t *testing.T) {
	c := New(10, nil)
	c.Set(1, testMsg)
	c.Set(2, testMsg)
	want := Stats{Capacity: 10, Size: 2}
	got := c.Stats()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Stats() = %+v, want %+v", got, want)
	}
}

func BenchmarkNewKey(b *testing.B) {
	for n := 0; n < b.N; n++ {
		NewKey("key", 1, 1)
	}
}

func BenchmarkSet(b *testing.B) {
	c := New(4096, nil)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c.Set(uint32(n), &dns.Msg{})
	}
}

func BenchmarkGet(b *testing.B) {
	c := New(4096, nil)
	c.Set(uint32(1), &dns.Msg{})
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c.Get(uint32(1))
	}
}

func BenchmarkEviction(b *testing.B) {
	c := New(1, nil)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c.Set(uint32(n), &dns.Msg{})
		c.Get(uint32(n))
	}
}
