package cache

import (
	"encoding/binary"
	"hash/fnv"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Cache represents a cache of DNS entries. Use New to initialize a new cache.
type Cache struct {
	capacity   int
	now        func() time.Time
	maintainer *maintainer
	mu         sync.RWMutex
	wg         sync.WaitGroup
	entries    map[uint32]*Value
	keys       []uint32
}

type maintainer struct {
	interval time.Duration
	done     chan bool
}

func maintain(cache *Cache, interval time.Duration) {
	m := &maintainer{
		interval: interval,
		done:     make(chan bool),
	}
	cache.maintainer = m
	cache.wg.Add(1)
	go m.run(cache)
}

func (m *maintainer) run(cache *Cache) {
	defer cache.wg.Done()
	ticker := time.NewTicker(m.interval)
	for {
		select {
		case <-ticker.C:
			cache.deleteExpired()
		case <-m.done:
			ticker.Stop()
			return
		}
	}
}

// Value represents a value stored in the cache.
type Value struct {
	Question  string
	Qtype     uint16
	Answers   []string
	CreatedAt time.Time
	msg       *dns.Msg
}

// TTL returns the TTL of this cache value.
func (v *Value) TTL() time.Duration { return minTTL(v.msg) }

// New creates a new cache of given capacity. Stale cache entries are removed at expiryInterval.
func New(capacity int, expiryInterval time.Duration) *Cache {
	if capacity < 0 {
		capacity = 0
	}
	if expiryInterval == 0 {
		expiryInterval = 10 * time.Minute
	}
	cache := &Cache{
		now:      time.Now,
		capacity: capacity,
		entries:  make(map[uint32]*Value, capacity),
	}
	maintain(cache, expiryInterval)
	return cache
}

// NewKey creates a new cache key for the DNS name, qtype and qclass
func NewKey(name string, qtype, qclass uint16) uint32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	binary.Write(h, binary.LittleEndian, qtype)
	binary.Write(h, binary.LittleEndian, qclass)
	return h.Sum32()
}

// Close closes the cache.
func (c *Cache) Close() error {
	c.maintainer.done <- true
	c.wg.Wait()
	return nil
}

// Get returns the DNS message associated with key k. Get will return nil if any TTL in the answer section of the
// message is exceeded according to time t.
func (c *Cache) Get(k uint32) (*dns.Msg, bool) {
	v, ok := c.getValue(k)
	if !ok {
		return nil, false
	}
	return v.msg, true
}

func (c *Cache) getValue(k uint32) (*Value, bool) {
	c.mu.RLock()
	v, ok := c.entries[k]
	c.mu.RUnlock()
	if !ok || c.isExpired(v) {
		return nil, false
	}
	return v, true
}

// List returns the n most recent cache values.
func (c *Cache) List(n int) []*Value {
	values := make([]*Value, 0, n)
	c.mu.RLock()
	for i := len(c.keys) - 1; i >= 0; i-- {
		if len(values) == n {
			break
		}
		v, _ := c.getValue(c.keys[i])
		values = append(values, v)
	}
	c.mu.RUnlock()
	return values
}

// Set associated key k with the DNS message v. Message msg will expire from the cache according to its TTL. Setting a
// new key in a cache that has reached its capacity will remove the first key.
func (c *Cache) Set(k uint32, msg *dns.Msg) {
	if c.capacity == 0 {
		return
	}
	if !isCacheable(msg) {
		return
	}
	now := c.now()
	c.mu.Lock()
	if len(c.entries) == c.capacity && c.capacity > 0 {
		delete(c.entries, c.keys[0])
		c.keys = c.keys[1:]
	}
	c.entries[k] = &Value{
		Question:  question(msg),
		Answers:   answers(msg),
		Qtype:     qtype(msg),
		CreatedAt: now,
		msg:       msg,
	}
	c.keys = append(c.keys, k)
	c.mu.Unlock()
}

func qtype(msg *dns.Msg) uint16 { return msg.Question[0].Qtype }

func question(msg *dns.Msg) string { return msg.Question[0].Name }

func answers(msg *dns.Msg) []string {
	var answers []string
	for _, answer := range msg.Answer {
		switch v := answer.(type) {
		case *dns.A:
			answers = append(answers, v.A.String())
		case *dns.AAAA:
			answers = append(answers, v.AAAA.String())
		case *dns.MX:
			answers = append(answers, v.Mx)
		}
	}
	return answers
}
func (c *Cache) deleteExpired() {
	c.mu.Lock()
	for k, v := range c.entries {
		if c.isExpired(v) {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

func (c *Cache) isExpired(v *Value) bool {
	now := c.now()
	for _, answer := range v.msg.Answer {
		if now.After(v.CreatedAt.Add(ttl(answer))) {
			return true
		}
	}
	return false
}

func min(x, y uint32) uint32 {
	if x < y {
		return x
	}
	return y
}

func minTTL(m *dns.Msg) time.Duration {
	var ttl uint32 = 1<<32 - 1 //  avoids importing math.MaxUint32
	for _, answer := range m.Answer {
		ttl = min(answer.Header().Ttl, ttl)
	}
	for _, ns := range m.Ns {
		ttl = min(ns.Header().Ttl, ttl)
	}
	return time.Duration(ttl) * time.Second
}

func isCacheable(m *dns.Msg) bool {
	if minTTL(m) == 0 {
		return false
	}
	return m.Rcode == dns.RcodeSuccess || m.Rcode == dns.RcodeNameError
}

func ttl(rr dns.RR) time.Duration {
	ttlSecs := rr.Header().Ttl
	return time.Duration(ttlSecs) * time.Second
}
