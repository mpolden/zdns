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

// Cache is a persistent DNS cache. Values added to the cache are written to a SQL database.
type Cache struct {
	wg     sync.WaitGroup
	queue  chan query
	client *Client
	logger *log.Logger
}

// NewCache creates a new cache using client for persistence.
func NewCache(client *Client) *Cache {
	c := &Cache{
		queue:  make(chan query, 1024),
		client: client,
	}
	go c.readQueue()
	return c
}

// Close consumes any outstanding writes and closes the cache.
func (c *Cache) Close() error {
	c.wg.Wait()
	return nil
}

// Set queues a write associating value with key. Set is non-blocking, but read operations wait for any pending writes
// to complete before reading.
func (c *Cache) Set(key uint32, value cache.Value) {
	c.enqueue(query{op: setOp, key: key, value: value})
}

// Evict queues a removal of key. As Set, Evict is non-blocking.
func (c *Cache) Evict(key uint32) { c.enqueue(query{op: removeOp, key: key}) }

// Reset queues removal of all entries. As Set, Reset is non-blocking.
func (c *Cache) Reset() { c.enqueue(query{op: resetOp}) }

// Read returns all entries in the cache.
func (c *Cache) Read() []cache.Value {
	c.wg.Wait()
	entries, err := c.client.readCache()
	if err != nil {
		log.Print(err)
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
				c.logger.Fatalf("failed to pack value: %s", err)
			}
			if err := c.client.writeCacheValue(q.key, packed); err != nil {
				c.logger.Printf("failed to write key=%d data=%q: %s", q.key, packed, err)
			}
		case removeOp:
			if err := c.client.removeCacheValue(q.key); err != nil {
				c.logger.Printf("failed to remove key=%d: %s", q.key, err)
			}
		case resetOp:
			if err := c.client.truncateCache(); err != nil {
				c.logger.Printf("failed to truncate cache: %s", err)
			}
		default:
			c.logger.Printf("unhandled operation %d", q.op)
		}
		c.wg.Done()
	}
}
