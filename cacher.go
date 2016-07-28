package cacher

import (
	"bytes"
	"encoding/gob"
	"errors"
	"log"
	"os"
	"time"

	"github.com/boltdb/bolt"
)

var (
	ErrCacheExpired = errors.New("item is expired")
	ErrCacheMiss    = errors.New("item is not present")
	ErrCacheHit     = errors.New("item is present")
	ErrCacheBypass  = errors.New("item was not searched in cache")
)

// CacheMode determines which mode of operation should be applied when using the cache.
type CacheMode uint8

// Origin is a type of function that is used by the advanced cache operations.
// Its usage allows Cacher to be used as a reverse proxy.
type Origin func() ([]byte, error)

const (
	// ReadThroughMode will read from cache and in the case of a cache miss or expired, WILL NOT WRITE to the cache.
	ReadThroughMode CacheMode = iota
	// WriteThroughMode will read from cache and in the case of a cache miss or expired, WILL SET the cache key to origin.
	WriteThroughMode
	// RefreshMode will try to get the cached item. If it exists or is expired, it will overwrite it with origin. Otherwise, nothing happens.
	// This is used to refresh cache items if they already exist in cache.
	RefreshMode
	// BypassMode will simply invoke origin and no other Cacher methods.
	BypassMode
	// ReloadMode will bypass reading from cache but WILL SET the cache key to origin.
	// This is used to touch items into cache.
	ReloadMode
)

// CacheItem is the definition if a cached item.
type CacheItem struct {
	//The bytes to actually store.
	Body []byte
	//The Unix timestamp in seconds for which this item is valid.
	//When setting the cache item you will probably want to do something like: time.Now().Unix()+123, where 123 is the validity of the item.
	Expiration int64
}

// Equal is a utility method for comparing the equality between two cache items.
func (c *CacheItem) Equal(other CacheItem) bool {
	return c.Expiration == other.Expiration && bytes.Equal(c.Body, other.Body)
}

// zone representes a cache zone that is managed by the Cacher.
type zone struct {
	fullPath string
	db       *bolt.DB
}

func (z *zone) close() {
	z.db.Close()
}

func (z *zone) purge() error {
	z.close()
	return os.Remove(z.fullPath)
}

// Cacher manages a persistent cache with many zones.
// Managing the zones' size is not supported.
type Cacher struct {
	zones map[string]*zone
}

// NewCacher creates a new Cacher. It takes in the base path to write the zones to.
// A variable number of zoneNames may be specified. The names will be the actual filenames on the filesystem.
// Passing no zoneNames will cause NewCacher to panic.
func NewCacher(basePath string, zoneNames ...string) *Cacher {
	if len(zoneNames) == 0 {
		panic(errors.New("cannot create a Cacher with no zones"))
	}
	cc := Cacher{zones: make(map[string]*zone)}
	for _, z := range zoneNames {
		fullPath := basePath + "/" + z
		db, err := bolt.Open(fullPath, 0600, nil)
		if err != nil {
			log.Println("error creating cache zone", z, err)
			continue
		}
		cc.zones[z] = &zone{fullPath, db}
		err = db.Batch(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte("default"))
			return err
		})
		if err != nil {
			log.Println("could not create bucket?", err)
		}
	}
	return &cc
}

// Purge completely removes a zone file from the filesystem and Cacher will stop referencing it.
func (c *Cacher) Purge(zoneName string) {
	z := c.zones[zoneName]
	z.purge()
	c.zones[zoneName] = nil
}

// PurgeAll invokes Purge for all of the Cacher's zones.
func (c *Cacher) PurgeAll() {
	for n, _ := range c.zones {
		c.Purge(n)
	}
}

// Close closes all zones' handlers and does not remove any data. This should be invoked prior to shutdown.
func (c *Cacher) Close() {
	for _, z := range c.zones {
		z.close()
	}
}

// Set sets a CacheItem to the given zone with the given key.
func (c *Cacher) Set(zoneName, key string, item CacheItem) error {

	z, ok := c.zones[zoneName]
	if !ok {
		return errors.New("could not set data because zone" + zoneName + "not found")
	}

	var serializedItem bytes.Buffer
	enc := gob.NewEncoder(&serializedItem)
	err := enc.Encode(item)
	if err != nil {
		log.Println("encode error:", err)
		return err
	}

	err = z.db.Batch(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("default"))
		err = b.Put([]byte(key), serializedItem.Bytes())
		return err
	})

	return err

}

// Get retrieves a CacheItem indexed by zoneName and key, returning the item and the error ErrCacheHit.
// If the item does not exist, the error returned is ErrCacheMiss.
// If the item is expired, the error returned is ErrCacheExpired.
// If some other error occured, it will be returned.
func (c *Cacher) Get(zoneName, key string) (item CacheItem, err error) {

	var serializedItem []byte

	z, ok := c.zones[zoneName]
	if !ok {
		return CacheItem{}, ErrCacheMiss
	}

	err = z.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("default"))
		serializedItem = b.Get([]byte(key))
		return nil
	})
	//some error occurred while consulting disk
	if err != nil {
		return CacheItem{}, err
	}
	//nothing was found on the disk
	if serializedItem == nil {
		return CacheItem{}, ErrCacheMiss
	}

	dec := gob.NewDecoder(bytes.NewBuffer(serializedItem))
	err = dec.Decode(&item)
	if item.Expiration < time.Now().Unix() {
		//item is expired
		return item, ErrCacheExpired
	}
	if err != nil {
		return CacheItem{}, err
	}
	return item, ErrCacheHit

}

// Deletes an item from the cache.
func (c *Cacher) Delete(zoneName, key string) {
	db := c.zones[zoneName].db
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("default"))
		err := b.Delete([]byte(key))
		return err
	})

}

func (c *Cacher) readWriteThrough(orig Origin, key, zoneName string, expires int64, store bool) (CacheItem, error) {
	var merr, lerr error
	var jj []byte
	item, merr := c.Get(zoneName, key)
	if merr == ErrCacheMiss || merr == ErrCacheExpired {
		//if we reqwuire extra logic for cacheexpired, we do it here!
		//its a cache miss or expired. we check origin
		jj, lerr = orig()
		if lerr == nil {
			//we found it in origin
			// then we set it in cache
			newItem := CacheItem{Body: jj, Expiration: time.Now().Unix() + expires}
			if store {
				c.Set(zoneName, key, newItem)
			}
			//cache miss or expired
			return newItem, merr
		} else {
			//we did not find it in origin; this item does not exist anywhere
			return CacheItem{}, lerr
		}
	}
	//cache hit
	return item, merr
}

// WriteThrough reads from cache and stores the result from origin in cache.
func (c *Cacher) WriteThrough(orig Origin, zoneName, key string, expires int64) (CacheItem, error) {
	return c.readWriteThrough(orig, key, zoneName, expires, true)
}

// ReadThrough reads from cache but does not store result in cache.
func (c *Cacher) ReadThrough(orig Origin, zoneName, key string, expires int64) (CacheItem, error) {
	return c.readWriteThrough(orig, key, zoneName, expires, false)
}

// Reload does not read from cache but sets the result in cache.
func (c *Cacher) Reload(orig Origin, zoneName, key string, expires int64) (CacheItem, error) {
	jj, err := orig()
	if err != nil {
		return CacheItem{}, err
	}
	cc := CacheItem{Body: jj, Expiration: time.Now().Unix() + expires}
	c.Set(zoneName, key, cc)
	return cc, ErrCacheBypass
}

// Refresh refreshes the item if it exists. Returns nil if refreshed or ErrCacheMiss if nothing exists to refresh.
func (c *Cacher) Refresh(orig Origin, zoneName, key string, expires int64) (CacheItem, error) {
	_, err := c.Get(zoneName, key)
	if err == ErrCacheExpired || err == ErrCacheHit {
		jj, err := orig()
		if err != nil {
			return CacheItem{}, err
		}
		cc := CacheItem{Body: jj, Expiration: time.Now().Unix() + expires}
		c.Set(zoneName, key, cc)
		return cc, nil //refreshed with success
	}
	return CacheItem{}, ErrCacheMiss
}

// Bypass completely ignores the cache and invokes origin directly.
func (c *Cacher) Bypass(orig Origin, zoneName, key string, expires int64) (CacheItem, error) {
	jj, err := orig()
	if err != nil {
		return CacheItem{}, err
	}
	return CacheItem{Body: jj, Expiration: time.Now().Unix() + expires}, ErrCacheBypass
}

// PickMode returns one of the following functions: ReadThrough, WriteThrough, Refresh, Bypass or Reload depending on the CacheMode passed in.
// This is just a convenience. We pass it the mode and get the appropriate function that we can immediately invoke.
// Since all advanced functions have the same signature, changing the mode of operation is as simple as changing the argument to this function.
func (c *Cacher) PickMode(mode CacheMode) func(Origin, string, string, int64) (CacheItem, error) {
	// note that we do not switch on ReadThroughMode
	// that's because the function must have an unconditional return statement or the compiler will complain
	switch mode {
	case WriteThroughMode:
		return c.WriteThrough
	case RefreshMode:
		return c.Refresh
	case BypassMode:
		return c.Bypass
	case ReloadMode:
		return c.Reload
	}
	return c.ReadThrough //default is readthrough
}
