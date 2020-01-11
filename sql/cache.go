package sql

import (
	"log"
	"sync"

	"github.com/mpolden/zdns/cache"
)

const (
	setOp = iota
	removeOp
	resetOp
)

type query struct {
	op    int
	key   uint32
	value cache.Value
}

// Cache is a persistent cache. Entries are written to a SQL database.
type Cache struct {
	wg     sync.WaitGroup
	queue  chan query
	client *Client
	logger *log.Logger
}

// NewCache creates a new cache using client to persist entries.
func NewCache(client *Client, logger *log.Logger) *Cache {
	c := &Cache{
		queue:  make(chan query, 100),
		client: client,
		logger: logger,
	}
	go c.readQueue()
	return c
}

// Close drains and persist queued requests in this cache.
func (c *Cache) Close() error {
	c.wg.Wait()
	return nil
}

// Set associates the value v with key.
func (c *Cache) Set(key uint32, v cache.Value) { c.enqueue(query{op: setOp, key: key, value: v}) }

// Evict removes any value associated with key.
func (c *Cache) Evict(key uint32) { c.enqueue(query{op: removeOp, key: key}) }

// Reset removes all entries from the cache.
func (c *Cache) Reset() { c.enqueue(query{op: resetOp}) }

// Read returns all entries in the cache.
func (c *Cache) Read() []cache.Value {
	c.wg.Wait()
	entries, err := c.client.readCache()
	if err != nil {
		c.logger.Print(err)
		return nil
	}
	values := make([]cache.Value, 0, len(entries))
	for _, entry := range entries {
		unpacked, err := cache.Unpack(entry.Data)
		if err != nil {
			panic(err) // Should never happen
		}
		values = append(values, unpacked)
	}
	return values
}

func (c *Cache) enqueue(q query) {
	c.wg.Add(1)
	c.queue <- q
}

func (c *Cache) readQueue() {
	for q := range c.queue {
		switch q.op {
		case setOp:
			packed, err := q.value.Pack()
			if err != nil {
				c.logger.Fatalf("failed to pack value: %w", err)
			}
			if err := c.client.writeCacheValue(q.key, packed); err != nil {
				c.logger.Printf("failed to write key=%d data=%q: %w", q.key, packed, err)
			}
		case removeOp:
			if err := c.client.removeCacheValue(q.key); err != nil {
				c.logger.Printf("failed to remove key=%d: %w", q.key, err)
			}
		case resetOp:
			if err := c.client.truncateCache(); err != nil {
				c.logger.Printf("failed to truncate cache: %w", err)
			}
		default:
			c.logger.Printf("unhandled operation %d", q.op)
		}
		c.wg.Add(-1)
	}
}
