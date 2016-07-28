package cacher

import (
	"errors"
	"log"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

func _buildItem(expiration int64) CacheItem {
	body := []byte("{\"cenas\":\"coisas\"}")
	return CacheItem{body, expiration}
}

func testOrigin() ([]byte, error) {
	return []byte("{\"cenas\":\"coisas\"}"), nil
}

func testOriginWithError() ([]byte, error) {
	return nil, errors.New("something bad happened in origin")
}

func pwd() string {
	dir, _ := os.Getwd()
	return dir
}

func TestNewCacher(t *testing.T) {
	cacher := NewCacher(pwd(), "teste.db")
	defer cacher.PurgeAll()
}

func TestGetSet(t *testing.T) {
	singleZone := "teste.getset"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item := _buildItem(time.Now().Unix() + 1000)

	err := cacher.Set(singleZone, "key1", item)
	if err != nil {
		t.Error(err)
	}

	ii, err := cacher.Get(singleZone, "key1")
	if err != nil && err != ErrCacheHit {
		t.Error(err)
	}

	if !ii.Equal(item) {
		log.Println(ii)
		log.Println(item)
		t.Error("returned items are not the same")
	}

}

func TestOverwrite(t *testing.T) {
	singleZone := "teste.overwrite"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item1 := _buildItem(time.Now().Unix() + 1000)

	item2 := _buildItem(time.Now().Unix() + 1001)

	err := cacher.Set(singleZone, "key1", item1)
	if err != nil {
		t.Error(err)
	}
	err = cacher.Set(singleZone, "key1", item2)
	if err != nil {
		t.Error(err)
	}

	ii, err := cacher.Get(singleZone, "key1")
	if err != nil && err != ErrCacheHit {
		t.Error(err)
	}
	if !ii.Equal(item2) {
		log.Println(ii)
		log.Println(item2)
		t.Error("could not update item")
	}

}

func TestMissing(t *testing.T) {
	singleZone := "teste.missing"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item1 := _buildItem(time.Now().Unix() + 1000)
	err := cacher.Set(singleZone, "key1", item1)
	if err != nil {
		t.Error(err)
	}
	ii, err := cacher.Get(singleZone, "key2")
	if err == ErrCacheMiss {
		return
	}
	log.Println(ii)
	t.Error("missing item misteriously exists or some other error occurred", err)

}

func TestExpired(t *testing.T) {
	singleZone := "teste.expired"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item1 := _buildItem(time.Now().Unix() - 100)
	err := cacher.Set(singleZone, "key1", item1)
	if err != nil {
		t.Error(err)
	}
	_, err = cacher.Get(singleZone, "key1")
	if err != nil && err != ErrCacheExpired {
		t.Error(err)
	}

}

func TestDelete(t *testing.T) {
	singleZone := "teste.delete"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item1 := _buildItem(time.Now().Unix() + 1000)
	err := cacher.Set(singleZone, "key1", item1)
	if err != nil {
		t.Error(err)
	}
	cacher.Delete(singleZone, "key1")
	ii, err := cacher.Get(singleZone, "key1")
	if err == ErrCacheMiss {
		return
	}
	log.Println(ii)
	t.Error("item not deleted", err)

}

func TestWriteThrough(t *testing.T) {
	singleZone := "teste.writehrough"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()

	item, err := cacher.WriteThrough(testOrigin, singleZone, "key1", 100)
	if err != ErrCacheMiss {
		t.Error(err)
		return
	}
	storedItem, err := cacher.Get(singleZone, "key1")
	if err != ErrCacheHit {
		log.Println(err)
		t.Error(errors.New("this should have been a cache hit"))
		return
	}
	if !item.Equal(storedItem) {
		t.Error("returned items are not the same")
		log.Println(item)
		log.Println(storedItem)
		return
	}

}

func TestReadThrough(t *testing.T) {
	singleZone := "teste.readthrough"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()

	_, err := cacher.ReadThrough(testOrigin, singleZone, "key1", 100)
	if err != ErrCacheMiss {
		t.Error(err)
		return
	}
	_, err = cacher.Get(singleZone, "key1")
	if err != ErrCacheMiss {
		t.Error(errors.New("this should have been a cache miss"))
		return
	}
}

func TestReload(t *testing.T) {
	singleZone := "teste.reload"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()

	item, err := cacher.Reload(testOrigin, singleZone, "key1", 100)
	if err != ErrCacheBypass {
		t.Error(err)
		return
	}
	storedItem, err := cacher.Get(singleZone, "key1")
	if err != ErrCacheHit {
		log.Println(err)
		t.Error(errors.New("this should have been a cache hit"))
		return
	}
	if !item.Equal(storedItem) {
		t.Error("returned items are not the same")
		log.Println(item)
		log.Println(storedItem)
		return
	}

}

func TestRefresh(t *testing.T) {
	singleZone := "teste.refresh"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()

	//the item does not exist yet, so we get cache miss and do nothing
	item, err := cacher.Refresh(testOrigin, singleZone, "key1", 100)
	if err != ErrCacheMiss {
		t.Error(err)
		return
	}

	//we now set something there and attempt to refresh it
	cacher.Set(singleZone, "key1", item)
	item, err = cacher.Refresh(testOrigin, singleZone, "key1", 100)
	if err != nil {
		t.Error(err)
		return
	}
	storedItem, err := cacher.Get(singleZone, "key1")
	if err != ErrCacheHit {
		log.Println(err)
		t.Error(errors.New("this should have been a cache hit"))
		return
	}
	if !item.Equal(storedItem) {
		t.Error("returned items are not the same")
		log.Println(item)
		log.Println(storedItem)
		return
	}

}

func TestOriginHasError(t *testing.T) {
	singleZone := "teste.origerror"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()

	for _, cachemode := range []CacheMode{ReadThroughMode, WriteThroughMode, RefreshMode, BypassMode, ReloadMode} {
		_, err := cacher.PickMode(cachemode)(testOriginWithError, singleZone, "key1", 100)
		if err == nil {
			t.Error(errors.New("we should have an error here"))
		}
		_, err = cacher.Get(singleZone, "key1")
		if err != ErrCacheMiss {
			t.Error(errors.New("this should have been a cache miss"))
		}
	}

}

func BenchmarkGetSerial(b *testing.B) {
	b.ReportAllocs()
	singleZone := "teste.getserial"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item := _buildItem(time.Now().Unix() + 1000)
	cacher.Set(singleZone, "key1", item)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		cacher.Get(singleZone, "key1")
	}
}

func BenchmarkSetSerial(b *testing.B) {
	b.ReportAllocs()
	singleZone := "teste.setserial"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item := _buildItem(time.Now().Unix() + 1000)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		cacher.Set(singleZone, "key"+strconv.Itoa(n), item)
	}
}

func BenchmarkGetMulti(b *testing.B) {
	b.ReportAllocs()
	singleZone := "teste.getmulti"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item := _buildItem(time.Now().Unix() + 1000)
	var wg sync.WaitGroup
	var keyList []string
	wg.Add(10000)
	for i := 0; i < 10000; i++ {
		k := strconv.Itoa(i)
		keyList = append(keyList, "key"+k)
		go func() {
			defer wg.Done()
			cacher.Set(singleZone, k, item)
		}()
	}
	wg.Wait()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cacher.Get(singleZone, keyList[rand.Intn(len(keyList))])
		}
	})
}

func BenchmarkSetMulti(b *testing.B) {
	b.ReportAllocs()
	singleZone := "teste.setmulti"
	cacher := NewCacher(pwd(), singleZone)
	defer cacher.PurgeAll()
	item := _buildItem(time.Now().Unix() + 1000)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cacher.Set(singleZone, "key"+strconv.Itoa(rand.Int()), item)
		}
	})
}
