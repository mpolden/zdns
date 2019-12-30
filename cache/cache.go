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
	done     chan bool
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

// New creates a new cache of given capacity. If client is non-nil, the cache will prefetch expired entries in an effort
// to serve results faster.
func New(capacity int, client *dnsutil.Client) *Cache {
	return newCache(capacity, client, 10*time.Second, time.Now)
}

func newCache(capacity int, client *dnsutil.Client, interval time.Duration, now func() time.Time) *Cache {
	if capacity < 0 {
		capacity = 0
	}
	cache := &Cache{
		client:   client,
		now:      now,
		capacity: capacity,
		values:   make(map[uint64]*Value, capacity),
		done:     make(chan bool),
	}
	go maintain(cache, interval)
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
			if cache.prefetch() {
				cache.refreshExpired(interval)
			} else {
				cache.evictExpired()
			}
		}
	}
}

// Close stops any outstanding maintenance tasks.
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
	if !ok || (!c.prefetch() && c.isExpired(v)) {
		return nil, false
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

// Set associates key k with the DNS message msg. Message msg will expire from the cache according to its TTL. Setting a
// new key in a cache that has reached its capacity will evict values in a FIFO order.
func (c *Cache) Set(k uint64, msg *dns.Msg) {
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
	c.values[k] = &Value{CreatedAt: now, msg: msg}
	c.keys = append(c.keys, k)
}

// Reset removes all values contained in cache c.
func (c *Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = make(map[uint64]*Value)
	c.keys = nil
}

func (c *Cache) prefetch() bool { return c.client != nil }

func (c *Cache) refreshExpired(interval time.Duration) {
	// TODO: Reduce lock contention for large caches. Consider sync.Map
	c.mu.Lock()
	defer c.mu.Unlock()
	evicted := make(map[uint64]bool)
	for k, v := range c.values {
		// Value will expire before the next interval. Refresh now
		if c.isExpiredAfter(interval, v) {
			q := v.msg.Question[0]
			msg := dns.Msg{}
			msg.SetQuestion(q.Name, q.Qtype)
			r, err := c.client.Exchange(&msg)
			if err != nil {
				continue // Will be retried on next run
			}
			if canCache(r) {
				c.values[k].CreatedAt = c.now()
				c.values[k].msg = r
			} else {
				// Can no longer be cached. Evict
				delete(c.values, k)
				evicted[k] = true
			}
		}
	}
	c.reorderKeys(evicted)
}

func (c *Cache) evictExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	evicted := make(map[uint64]bool)
	for k, v := range c.values {
		if c.isExpired(v) {
			delete(c.values, k)
			evicted[k] = true
		}
	}
	c.reorderKeys(evicted)
}

func (c *Cache) reorderKeys(evicted map[uint64]bool) {
	if len(evicted) == 0 {
		return
	}
	// At least one entry was evicted. The ordered list of keys must be updated.
	var keys []uint64
	for _, k := range c.keys {
		if _, ok := evicted[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	c.keys = keys
}

func (c *Cache) isExpiredAfter(d time.Duration, v *Value) bool {
	expiresAt := v.CreatedAt.Add(dnsutil.MinTTL(v.msg))
	return c.now().Add(d).After(expiresAt)
}

func (c *Cache) isExpired(v *Value) bool { return c.isExpiredAfter(0, v) }

func canCache(msg *dns.Msg) bool {
	if dnsutil.MinTTL(msg) == 0 {
		return false
	}
	return msg.Rcode == dns.RcodeSuccess || msg.Rcode == dns.RcodeNameError
}
