package cache

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
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
	entries    map[uint32]*value
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
	go m.run(cache)
}

func (m *maintainer) run(cache *Cache) {
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

type value struct {
	msg       *dns.Msg
	createdAt time.Time
}

// New creates a new cache with a maximum size of maxSize. Stale cache entries are removed at expiryInterval.
func New(maxSize int, expiryInterval time.Duration) (*Cache, error) {
	if maxSize < 0 {
		return nil, fmt.Errorf("invalid cache size: %d", maxSize)
	}
	if expiryInterval <= 0 {
		return nil, fmt.Errorf("invalid expiry interval: %d", expiryInterval)
	}
	cache := &Cache{
		now:     time.Now,
		maxSize: maxSize,
		entries: make(map[uint32]*value),
	}
	maintain(cache, expiryInterval)
	return cache, nil
}

// NewKey creates a new cache key for the DNS name, qtype and qclass
func NewKey(name string, qtype, qclass uint16) uint32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	_ = binary.Write(h, binary.LittleEndian, qtype)
	_ = binary.Write(h, binary.LittleEndian, qclass)
	return h.Sum32()
}

// Close closes the cache.
func (c *Cache) Close() error {
	c.maintainer.done <- true
	return nil
}

// Get returns the DNS message associated with key k. Get will return nil if any TTL in the answer section of the
// message is exceeded according to time t.
func (c *Cache) Get(k uint32) (*dns.Msg, bool) {
	c.mu.RLock()
	v, ok := c.entries[k]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if c.isExpired(v) {
		return nil, false
	}
	return v.msg, true
}

// Set associated key k with the DNS message v. Message v will expire from the cache according to its TTL. Setting a
// new key in a cache that has  its maximum size will remove the first key.
func (c *Cache) Set(k uint32, v *dns.Msg) {
	if c.maxSize == 0 {
		return
	}
	now := c.now()
	c.mu.Lock()
	if len(c.entries) == c.maxSize && c.maxSize > 0 {
		delete(c.entries, c.keys[0])
		c.keys = c.keys[1:]
	}
	c.entries[k] = &value{v, now}
	c.keys = append(c.keys, k)
	c.mu.Unlock()
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

func (c *Cache) isExpired(v *value) bool {
	now := c.now()
	for _, answer := range v.msg.Answer {
		if now.After(v.createdAt.Add(ttl(answer))) {
			return true
		}
	}
	return false
}

func ttl(rr dns.RR) time.Duration {
	ttlSecs := rr.Header().Ttl
	return time.Duration(time.Duration(ttlSecs) * time.Second)
}
