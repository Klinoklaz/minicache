package cache

import (
	"container/list"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/klinoklaz/minicache/helper"
)

type Cache struct {
	Header  http.Header
	Content []byte

	ready       chan bool
	keys        []string // cache pool key
	accessCnt   int
	hash        [16]byte
	status      byte
	protectedAt time.Time
}

func (c *Cache) String() string {
	return fmt.Sprintf("URIs: %v, access count: %d, status: %c, content length: %d, hash: %s, protected at: %s",
		c.keys,
		c.accessCnt,
		c.status,
		len(c.Content),
		hex.EncodeToString(c.hash[:]),
		c.protectedAt.Format(time.StampMicro))
}

// cache entry status
const (
	fresh   byte = 'f'
	protect byte = 'p'
	stale   byte = 's'
	invalid byte = 'i'
)

var cachePool struct {
	pool          map[string]*Cache
	size          int
	mtx           sync.RWMutex
	hashes        map[[16]byte]*Cache // stores content md5 sum in deduplicate mode
	evictorWakeup chan bool
}

// can not rely on package init cuz we have to wait for config to be loaded
func Init() {
	cachePool.evictorWakeup = make(chan bool)
	cachePool.pool = make(map[string]*Cache)
	if helper.Config.CacheUnique {
		cachePool.hashes = make(map[[16]byte]*Cache)
	}

	protectList.li = list.New()
	go lfuEvict()
	go cacheStale()
}

func keygen(r *http.Request) string {
	if helper.Config.CacheMobile && strings.Contains(r.Header.Get("User-Agent"), "Mobi") {
		return "_" + r.Method[0:2] + "_" + r.RequestURI
	}
	return r.Method[0:2] + "_" + r.RequestURI
}

// forwads request to proxy target and fills up cache entry's fields using the response
func (c *Cache) newRequest(r *http.Request) *http.Response {
	r.Header.Del("Authorization")
	r.Header.Del("Cookie")

	res, err := helper.DoRequest(r)
	if err != nil {
		helper.Log(helper.LogErr, "caching target unreachable, %s %s #%s", r.Method, r.RequestURI, err)
		return nil
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		c.status = invalid
	} else {
		c.status = fresh
	}

	c.accessCnt = 1
	res.Header.Del("Set-Cookie")
	res.Header.Del("Expires")
	c.Header = res.Header.Clone()

	c.Content, err = io.ReadAll(res.Body)
	if err != nil {
		helper.Log(helper.LogErr, "could not read response for caching, %s %s #%s", r.Method, r.RequestURI, err)
	} else if helper.Config.CacheUnique {
		c.hash = md5.Sum(c.Content)
	}

	return res
}

// get cache entry
func Get(r *http.Request) (*Cache, *http.Response) {
	ctx := r.Context()
	key := keygen(r)

	cachePool.mtx.RLock()
	c := cachePool.pool[key]

	if c != nil && c.status != invalid {
		cachePool.mtx.RUnlock()
		countAccess(c, ctx)
		return c, nil
	}

	cachePool.mtx.RUnlock()
	cachePool.mtx.Lock()

	// check again since there's a time window in lock escalation
	c = cachePool.pool[key]
	if c != nil && c.status != invalid {
		cachePool.mtx.Unlock()
		countAccess(c, ctx)
		return c, nil
	}

	// first request or retry
	c = &Cache{ready: make(chan bool), keys: []string{key}}
	cachePool.pool[key] = c
	cachePool.mtx.Unlock()

	res := c.newRequest(r)

	if c.status != invalid && cachePool.size < helper.Config.CacheSize {
		accept(c)
	} else {
		cachePool.mtx.Lock()
		delete(cachePool.pool, key)
		cachePool.mtx.Unlock()
		// if we allow new cache to be accepted here,
		// protected list may grow infinitely before anything being moved to LFU list
		if c.status != invalid {
			go func() { cachePool.evictorWakeup <- true }()
			helper.Log(helper.LogInfo, "new cache can not be added because cache pool is already full.")
		}
	}
	close(c.ready)

	return c, res
}

func accept(c *Cache) {
	cachePool.mtx.Lock()

	cachePool.size += len(c.Content)

	if !helper.Config.CacheUnique {
		cachePool.mtx.Unlock()
		protectList.protect(c)
		return
	}

	// ensure same response data being added only once to the pool
	if cc, ok := cachePool.hashes[c.hash]; ok {
		cachePool.pool[c.keys[0]] = cc
		cc.keys = append(cc.keys, c.keys[0])
		cachePool.mtx.Unlock()
		helper.Log(helper.LogDebug, "found duplicated content for %s, merge into existing one. %s", c.keys[0], cc)
	} else {
		cachePool.hashes[c.hash] = c
		cachePool.mtx.Unlock()
		protectList.protect(c)
		helper.Log(helper.LogDebug, "new cache entry added. %s", c)
	}
}

// don't put this inside cache pool's mutex section,
// or it will create dead locks with accept()
func countAccess(c *Cache, ctx context.Context) {
	select {
	case <-c.ready:
	case <-ctx.Done():
		return
	}

	// use protectedAt as starting point of the counting period makes sense
	// because its first value is very close to the creation time of c.
	// access count doesn't need to be accurate, so no locking on individual entry
	if time.Since(c.protectedAt) <= helper.Config.LfuTime {
		c.accessCnt++
		return
	}

	c.accessCnt = 1
	// NOTE: race, potentially can cause duplicated entries in the list,
	// but kinda ok
	if c.status != protect {
		reprotectList.protect(c)
	}
	// there's no way to remove c from LFU list after reprotecting it
	// since we don't know its index, this also leads to duplicated entries in LFU list.
	// doesn't matter cuz LFU list will be sorted anyway
}
