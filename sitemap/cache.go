package sitemap

import (
	"sync"
	"time"
)

// docCache is a tiny TTL cache of rendered documents keyed by route. A zero TTL
// disables caching (every request regenerates), which is the right default for
// small sources; larger sources set a TTL so a crawler burst does not re-run
// the source on every fetch. now is injectable for tests.
type docCache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	body []byte
	at   time.Time
}

func newDocCache(ttl time.Duration) *docCache {
	return &docCache{ttl: ttl, now: time.Now, entries: map[string]cacheEntry{}}
}

// get returns the cached body for key when fresh, otherwise calls gen, stores
// its result, and returns it. gen runs outside the lock is *not* guaranteed --
// a brief lock is held across generation, which serializes concurrent misses
// for the same leaf. Sitemap generation is infrequent enough that the
// simplicity is worth more than per-key locking.
func (c *docCache) get(key string, gen func() ([]byte, error)) ([]byte, error) {
	if c.ttl <= 0 {
		return gen()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok && c.now().Sub(e.at) < c.ttl {
		return e.body, nil
	}
	body, err := gen()
	if err != nil {
		return nil, err
	}
	c.entries[key] = cacheEntry{body: body, at: c.now()}
	return body, nil
}
