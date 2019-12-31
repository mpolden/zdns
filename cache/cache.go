package cache

import (
	"encoding/binary"
	"hash/fnv"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/dns/dnsutil"
)

// Cache is a cache of DNS messages.
type Cache struct {
	client   *dnsutil.Client
	capacity int
	values   map[uint64]*Value
	keys     []uint64
	mu       sync.RWMutex
	now      func() time.Time
}

// Value wraps a DNS message stored in the cache.
type Value struct {
	CreatedAt time.Time
	msg       *dns.Msg
}

// Rcode returns the response code of the cached value v.
func (v *Value) Rcode() int { return v.msg.Rcode }

// Question returns the first question the cached value v.
func (v *Value) Question() string { return v.msg.Question[0].Name }

// Qtype returns the query type of the cached value v
func (v *Value) Qtype() uint16 { return v.msg.Question[0].Qtype }

// Answers returns the answers of the cached value v.
func (v *Value) Answers() []string { return dnsutil.Answers(v.msg) }

// TTL returns the time to live of the cached value v.
func (v *Value) TTL() time.Duration { return dnsutil.MinTTL(v.msg) }

// New creates a new cache of given capacity.
//
// If client is non-nil, the cache will prefetch expired entries in an effort to serve results faster.
func New(capacity int, client *dnsutil.Client) *Cache {
	return newCache(capacity, client, time.Now)
}

func newCache(capacity int, client *dnsutil.Client, now func() time.Time) *Cache {
	if capacity < 0 {
		capacity = 0
	}
	return &Cache{
		client:   client,
		now:      now,
		capacity: capacity,
		values:   make(map[uint64]*Value, capacity),
	}
}

// NewKey creates a new cache key for the DNS name, qtype and qclass
func NewKey(name string, qtype, qclass uint16) uint64 {
	h := fnv.New64a()
	h.Write([]byte(name))
	binary.Write(h, binary.BigEndian, qtype)
	binary.Write(h, binary.BigEndian, qclass)
	return h.Sum64()
}

// Get returns the DNS message associated with key.
func (c *Cache) Get(key uint64) (*dns.Msg, bool) {
	v, ok := c.getValue(key)
	if !ok {
		return nil, false
	}
	return v.msg, true
}

func (c *Cache) getValue(key uint64) (*Value, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.values[key]
	if !ok {
		return nil, false
	}
	if c.isExpired(v) {
		if !c.prefetch() {
			go c.evictWithLock(key)
			return nil, false
		}
		// Refresh and return a stale value
		go c.refresh(key, v.msg)
	}
	return v, true
}

// List returns the n most recent values in cache c.
func (c *Cache) List(n int) []Value {
	values := make([]Value, 0, n)
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := len(c.keys) - 1; i >= 0; i-- {
		if len(values) == n {
			break
		}
		v, ok := c.getValue(c.keys[i])
		if !ok {
			continue
		}
		values = append(values, *v)
	}
	return values
}

// Set associates key with the DNS message msg.
//
// If prefetching is disabled, the message will be evicted from the cache according to its TTL.
//
// If prefetching is enabled, the message will never be evicted, but it will be refreshed when the TTL passes.
//
// Setting a new key in a cache that has reached its capacity will evict values in a FIFO order.
func (c *Cache) Set(key uint64, msg *dns.Msg) {
	if c.capacity == 0 {
		return
	}
	if !canCache(msg) {
		return
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.values) == c.capacity && c.capacity > 0 {
		delete(c.values, c.keys[0])
		c.keys = c.keys[1:]
	}
	c.values[key] = &Value{CreatedAt: now, msg: msg}
	c.keys = append(c.keys, key)
}

// Reset removes all values contained in cache c.
func (c *Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = make(map[uint64]*Value)
	c.keys = nil
}

func (c *Cache) prefetch() bool { return c.client != nil }

func (c *Cache) refresh(key uint64, old *dns.Msg) {
	q := old.Question[0]
	msg := dns.Msg{}
	msg.SetQuestion(q.Name, q.Qtype)
	r, err := c.client.Exchange(&msg)
	if err != nil {
		return // Retry on next request
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if canCache(r) {
		c.values[key].CreatedAt = c.now()
		c.values[key].msg = r
	} else {
		c.evict(key)
	}
}

func (c *Cache) evictWithLock(key uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evict(key)
}

func (c *Cache) evict(key uint64) {
	delete(c.values, key)
	var keys []uint64
	for _, k := range c.keys {
		if k == key {
			continue
		}
		keys = append(keys, k)
	}
	c.keys = keys
}

func (c *Cache) isExpired(v *Value) bool {
	expiresAt := v.CreatedAt.Add(dnsutil.MinTTL(v.msg))
	return c.now().After(expiresAt)
}

func canCache(msg *dns.Msg) bool {
	if dnsutil.MinTTL(msg) == 0 {
		return false
	}
	return msg.Rcode == dns.RcodeSuccess || msg.Rcode == dns.RcodeNameError
}
