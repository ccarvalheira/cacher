# Cacher

Cacher is a simple embedded caching mechanism for caching stuff in your Go applications. It supports some advanced reverse proxy modes of operation such as read/write through, touch and refresh. It is not prepared to be a networked server on its own, but you may wrap it around a mux/http server should you need it.

It uses boltdb for presistence: https://github.com/boltdb/bolt

## Installation

Get the dependencies and the code:
```Bash
go get github.com/boltdb/bolt
go get github.com/ccarvalheira/cacher
```

## Usage

### Simple usage:
```Go
import "github.com/ccarvalheira/cacher"
import "time"
(...)
cache := cacher.NewCacher("/tmp", "zoneOne", "zoneTwo")
defer cache.Close()

expiration := int64(100) //in seconds
item := cache.CacheItem{[]byte("data goes here"), time.Now().Unix()+expiration}
err := cache.Set("zoneOne", "key1", item)
if err != nil {
  log.Println("something went wrong:", err)
}
retrievedItem, err := cache.Get("zoneOne", "key1")

```

### Usage with reverse proxy stuff:

```Go
import "github.com/ccarvalheira/cacher"

func myOrigin() ([]byte, error) {
  //origin functions may be as complicated or as simple as you want
  //including making requests over the network, database calls, etc
  //if origin returns an error, cacher will stop the current operation and bubble the error up
  return []byte("some data here"), nil
}

cache := cacher.NewCacher("/tmp", "zoneOne", "zoneTwo")
defer cache.Close()


item, err := cache.WriteThrough(myOrigin, "zoneOne", "key1", 123)
storedItem, err := cache.Get("zoneOne", "key1")

log.Println(item.Equal(storedItem))

```

For more usage examples and information, please consult the cacher_test.go file.

## Other considerations

Currently, there is no mechanism for controlling the size of the cache. This should either be done manually or added later on. This implementation is designed to be embedded in an application and not a networked service.

Benchmarks on the code show that making concurrent requests to the cache results in much higher performance.

Compared to nginx cache, this implementation is 10~20% slower. This difference is likely due to the maturity of the performance optimizations in nginx compared to cacher and also because of the extra network hops inserted in the tests. These tests were conducted at my current company (at time of writing) with a particular architecture in mind so we had an idea of what the actual difference would be.
