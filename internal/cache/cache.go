// Package cache is a small bounded TTL cache with request coalescing: many
// concurrent misses on one key run the loader once and share its answer.
package cache

import (
	"container/list"
	"sync"
	"time"
)

type entry struct {
	key     string
	val     any
	err     error
	expires time.Time
}

type call struct {
	done chan struct{}
	val  any
	err  error
}

type Cache struct {
	mu       sync.Mutex
	max      int
	ll       *list.List // front = most recently used
	items    map[string]*list.Element
	inflight map[string]*call
}

func New(max int) *Cache {
	return &Cache{max: max, ll: list.New(), items: map[string]*list.Element{}, inflight: map[string]*call{}}
}

// Do returns the cached value for key, or runs load once (whatever the
// number of concurrent callers) and keeps its result for the TTL it returns.
// A zero TTL keeps nothing (use it for transient errors).
func Do[T any](c *Cache, key string, load func() (T, time.Duration, error)) (T, error) {
	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		if time.Now().Before(e.expires) {
			c.ll.MoveToFront(el)
			c.mu.Unlock()
			v, _ := e.val.(T)
			return v, e.err
		}
		c.ll.Remove(el)
		delete(c.items, key)
	}
	if cl, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-cl.done
		v, _ := cl.val.(T)
		return v, cl.err
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[key] = cl
	c.mu.Unlock()

	v, ttl, err := load()
	cl.val, cl.err = v, err
	close(cl.done)

	c.mu.Lock()
	delete(c.inflight, key)
	if ttl > 0 {
		c.items[key] = c.ll.PushFront(&entry{key: key, val: v, err: err, expires: time.Now().Add(ttl)})
		for c.ll.Len() > c.max {
			old := c.ll.Back()
			c.ll.Remove(old)
			delete(c.items, old.Value.(*entry).key)
		}
	}
	c.mu.Unlock()
	return v, err
}
