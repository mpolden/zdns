package cache

import (
	"encoding/binary"
	"hash/fnv"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Cache represents a cache of DNS responses. Use New to initialize a new cache.
type Cache struct {
	capacity int
	values   map[uint64]*Value
	keys     []uint64
	mu       sync.RWMutex
	done     chan bool
	now      func() time.Time
}

// Value represents a value stored in the cache.
type Value struct {
	CreatedAt time.Time
	msg       *dns.Msg
}

// Rcode returns the response code of this cached value.
func (v *Value) Rcode() int { return v.msg.Rcode }

// Question returns the question of this cached value.
func (v *Value) Question() string { return v.msg.Question[0].Name }

// Qtype returns the DNS request type of this cached value.
func (v *Value) Qtype() uint16 { return v.msg.Question[0].Qtype }

// Answers returns the DNS responses of this cached value.
func (v *Value) Answers() []string {
	var answers []string
	for _, answer := range v.msg.Answer {
		switch v := answer.(type) {
		case *dns.A:
			answers = append(answers, v.A.String())
		case *dns.AAAA:
			answers = append(answers, v.AAAA.String())
		case *dns.MX:
			answers = append(answers, v.Mx)
		case *dns.PTR:
			answers = append(answers, v.Ptr)
		}
	}
	return answers
}

// TTL returns the TTL of this cache value.
func (v *Value) TTL() time.Duration { return minTTL(v.msg) }

// New creates a new cache of given capacity. Stale cache values are removed every expiryInterval.
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
		values:   make(map[uint64]*Value, capacity),
		done:     make(chan bool),
	}
	go maintain(cache, expiryInterval)
	return cache
}

// NewKey creates a new cache key for the DNS name, qtype and qclass
func NewKey(name string, qtype, qclass uint16) uint64 {
	h := fnv.New64a()
	h.Write([]byte(name))
	binary.Write(h, binary.BigEndian, qtype)
	binary.Write(h, binary.BigEndian, qclass)
	return h.Sum64()
}

func maintain(cache *Cache, interval time.Duration) {
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-cache.done:
			ticker.Stop()
			return
		case <-ticker.C:
			cache.deleteExpired()
		}
	}
}

// Close closes the cache.
func (c *Cache) Close() error {
	c.done <- true
	return nil
}

// Get returns the DNS message associated with key k. Get will return nil if any TTL in the answer section of the
// message is exceeded according to time t.
func (c *Cache) Get(k uint64) (*dns.Msg, bool) {
	v, ok := c.getValue(k)
	if !ok {
		return nil, false
	}
	return v.msg, true
}

func (c *Cache) getValue(k uint64) (*Value, bool) {
	c.mu.RLock()
	v, ok := c.values[k]
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
		v, ok := c.getValue(c.keys[i])
		if !ok {
			continue
		}
		values = append(values, v)
	}
	c.mu.RUnlock()
	return values
}

// Set associated key k with the DNS message v. Message msg will expire from the cache according to its TTL. Setting a
// new key in a cache that has reached its capacity will remove the first key.
func (c *Cache) Set(k uint64, msg *dns.Msg) {
	if c.capacity == 0 {
		return
	}
	if !isCacheable(msg) {
		return
	}
	now := c.now()
	c.mu.Lock()
	if len(c.values) == c.capacity && c.capacity > 0 {
		delete(c.values, c.keys[0])
		c.keys = c.keys[1:]
	}
	c.values[k] = &Value{CreatedAt: now, msg: msg}
	c.keys = append(c.keys, k)
	c.mu.Unlock()
}

func (c *Cache) deleteExpired() {
	c.mu.Lock()
	for k, v := range c.values {
		if c.isExpired(v) {
			delete(c.values, k)
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
