package cache

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Cache represents a cache of DNS entries. Use New to initialize a new cache.
type Cache struct {
	maxSize    int
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
	Answer    net.IP
	CreatedAt time.Time
	msg       *dns.Msg
}

// TTL returns the TTL of this cache value.
func (v *Value) TTL() time.Duration { return minTTL(v.msg) }

// New creates a new cache with a maximum size of maxSize. Stale cache entries are removed at expiryInterval.
func New(maxSize int, expiryInterval time.Duration) (*Cache, error) {
	if maxSize < 0 {
		return nil, fmt.Errorf("invalid cache size: %d", maxSize)
	}
	if expiryInterval == 0 {
		expiryInterval = 10 * time.Minute
	}
	cache := &Cache{
		now:     time.Now,
		maxSize: maxSize,
		entries: make(map[uint32]*Value, maxSize),
	}
	maintain(cache, expiryInterval)
	return cache, nil
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
// new key in a cache that has reached its maximum size will remove the first key.
func (c *Cache) Set(k uint32, msg *dns.Msg) {
	if c.maxSize == 0 {
		return
	}
	if !isCacheable(msg) {
		return
	}
	now := c.now()
	c.mu.Lock()
	if len(c.entries) == c.maxSize && c.maxSize > 0 {
		delete(c.entries, c.keys[0])
		c.keys = c.keys[1:]
	}
	c.entries[k] = &Value{
		Question:  question(msg),
		Answer:    answer(msg),
		Qtype:     qtype(msg),
		CreatedAt: now,
		msg:       msg,
	}
	c.keys = append(c.keys, k)
	c.mu.Unlock()
}

func qtype(m *dns.Msg) uint16 { return m.Question[0].Qtype }

func question(m *dns.Msg) string { return m.Question[0].Name }

func answer(m *dns.Msg) net.IP {
	rr := m.Answer[0]
	switch v := rr.(type) {
	case *dns.A:
		return v.A
	case *dns.AAAA:
		return v.AAAA
	}
	return net.IPv4zero
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
