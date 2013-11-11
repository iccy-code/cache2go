// Simple caching library with expiration capabilities
package cache2go

import (
	"errors"
	"sync"
	"time"
)

// Structure of an entry in the cache
// data contains the user-set value in the cache
type CacheEntry struct {
	sync.Mutex
	key            string
	lifeSpan       time.Duration
	createdOn      time.Time
	accessedOn     time.Time
	data           interface{}

	// Callback method triggered right before removing the item from the cache
	aboutToExpire func(string)
}

// Structure of a table with items in the cache
type CacheTable struct {
	sync.RWMutex
	name            string
	items           map[string]*CacheEntry
	cleanupTimer    *time.Timer
	cleanupInterval time.Duration
}

var (
	cache = make(map[string]*CacheTable)
	mutex sync.RWMutex
)

// Mark entry to be kept for another expireDuration period
func (entry *CacheEntry) KeepAlive() {
	entry.Lock()
	defer entry.Unlock()
	entry.accessedOn = time.Now()
}

// Returns this entry's expiration duration
func (entry *CacheEntry) LifeSpan() time.Duration {
	entry.Lock()
	defer entry.Unlock()
	return entry.lifeSpan
}

// Returns when this entry was last accessed
func (entry *CacheEntry) AccessedOn() time.Time {
	entry.Lock()
	defer entry.Unlock()
	return entry.accessedOn
}

// Returns when this entry was added to the cache
func (entry *CacheEntry) CreatedOn() time.Time {
	entry.Lock()
	defer entry.Unlock()
	return entry.createdOn
}

// Returns the value of this cached item
func (entry *CacheEntry) Data() interface{} {
	entry.Lock()
	defer entry.Unlock()
	return entry.data
}

// Returns the existing cache table with given name or creates a new one
// if the table does not exist yet
func Cache(table string) *CacheTable {
	mutex.RLock()
	t, ok := cache[table]
	mutex.RUnlock()

	if !ok {
		t = &CacheTable{
			name:  table,
			items: make(map[string]*CacheEntry),
		}
		mutex.Lock()
		cache[table] = t
		mutex.Unlock()
	}

	return t
}

// Returns how many items are currently stored in the cache
func (table *CacheTable) CacheCount() int {
	table.RLock()
	defer table.RUnlock()

	return len(table.items)
}

// Expiration check loop, triggered by a self-adjusting timer
func (table *CacheTable) expirationCheck() {
	table.Lock()
	if table.cleanupTimer != nil {
		table.cleanupTimer.Stop()
	}

	// Take a copy of cache so we can iterate over it without blocking the mutex
	cc := table.items
	table.Unlock()

	// To be more accurate with timers, we would need to update 'now' on every
	// loop iteration. Not sure it's really efficient though.
	now := time.Now()
	smallestDuration := 0 * time.Second
	for key, c := range cc {
		if c.lifeSpan == 0 {
			continue
		}
		if now.Sub(c.accessedOn) >= c.lifeSpan {
			if c.aboutToExpire != nil {
				c.aboutToExpire(key)
			}
			table.Lock()
			delete(table.items, key)
			table.Unlock()
		} else {
			if smallestDuration == 0 || c.lifeSpan < smallestDuration {
				smallestDuration = c.lifeSpan - now.Sub(c.accessedOn)
			}
		}
	}

	table.Lock()
	table.cleanupInterval = smallestDuration
	if smallestDuration > 0 {
		table.cleanupTimer = time.AfterFunc(smallestDuration, func() {
			go table.expirationCheck()
		})
	}
	table.Unlock()
}

/* Adds a key/value pair to the cache
 / key is a unique cache-item key in the cache
 / lifeSpan indicates how long this item will remain in the cache after its
 / last access
 / data is the cache-item value
 / The last parameter abouToExpireFunc can be nil. Otherwise abouToExpireFunc
 / will be called (with this item's key as its only parameter), right before
 / removing this item from the cache
*/
func (table *CacheTable) Cache(key string, lifeSpan time.Duration, data interface{}, aboutToExpireFunc func(string)) {
	entry := CacheEntry{}
	entry.key = key
	entry.lifeSpan = lifeSpan
	entry.createdOn = time.Now()
	entry.accessedOn = entry.createdOn
	entry.aboutToExpire = aboutToExpireFunc
	entry.data = data

	table.Lock()
	table.items[key] = &entry
	expDur := table.cleanupInterval
	table.Unlock()

	// If we haven't set up any expiration check timer or found a more imminent item
	if lifeSpan > 0 && (expDur == 0 || lifeSpan < expDur) {
		table.expirationCheck()
	}
}

// Get an entry from the cache and mark it to be kept alive
func (table *CacheTable) Value(key string) (*CacheEntry, error) {
	table.RLock()
	defer table.RUnlock()
	if r, ok := table.items[key]; ok {
		r.KeepAlive()
		return r, nil
	}
	return nil, errors.New("Key not found in cache")
}

// Delete all items from cache
func (table *CacheTable) Flush() {
	table.Lock()
	defer table.Unlock()

	table.items = make(map[string]*CacheEntry)
	table.cleanupInterval = 0
	if table.cleanupTimer != nil {
		table.cleanupTimer.Stop()
	}

	mutex.Lock()
	defer mutex.Unlock()
	delete(cache, table.name)
}
