package cache

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Cache represents a cache of DNS entries
type Cache struct {
	maxSize int
	mu      sync.RWMutex
	entries map[uint32]value
	keys    []uint32
}

type value struct {
	msg       dns.Msg
	createdAt time.Time
}

// New creates a new cache with the given maximum size. Adding a key to a cache of this size removes the oldest key.
func New(maxSize int) (*Cache, error) {
	if maxSize < 0 {
		return nil, fmt.Errorf("invalid cache size: %d", maxSize)
	}
	return &Cache{
		maxSize: maxSize,
		entries: make(map[uint32]value),
	}, nil
}

// NewKey creates a new cache key for the DNS name, qtype and qclass
func NewKey(name string, qtype, qclass uint16) uint32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	_ = binary.Write(h, binary.LittleEndian, qtype)
	_ = binary.Write(h, binary.LittleEndian, qclass)
	return h.Sum32()
}

// Get returns the DNS message associated with key k. Get will return nil if any TTL in the answer section of the //
// message is exceeded according to time t.
func (c *Cache) Get(k uint32, t time.Time) (dns.Msg, bool) {
	c.mu.RLock()
	v, ok := c.entries[k]
	c.mu.RUnlock()
	if !ok {
		return dns.Msg{}, false
	}
	if isExpired(v, t) {
		c.mu.Lock()
		delete(c.entries, k)
		c.mu.Unlock()
		return dns.Msg{}, false
	}
	return v.msg, true
}

// Add adds given DNS message msg to the cache with creation time t. Creation time plus the TTL of the answer section
// decides when the message expires.
func (c *Cache) Add(msg *dns.Msg, t time.Time) {
	if c.maxSize == 0 {
		return
	}
	q := msg.Question[0]
	k := NewKey(q.Name, q.Qtype, q.Qclass)
	c.mu.Lock()
	if len(c.entries) == c.maxSize && c.maxSize > 0 {
		// Reached max size, delete the oldest entry
		delete(c.entries, c.keys[0])
		c.keys = c.keys[1:]
	}
	c.entries[k] = value{*msg, t}
	c.keys = append(c.keys, k)
	c.mu.Unlock()
}

func isExpired(v value, t time.Time) bool {
	for _, answer := range v.msg.Answer {
		if t.After(v.createdAt.Add(ttl(answer))) {
			return true
		}
	}
	return false
}

func ttl(rr dns.RR) time.Duration {
	ttlSecs := rr.Header().Ttl
	return time.Duration(time.Duration(ttlSecs) * time.Second)
}
