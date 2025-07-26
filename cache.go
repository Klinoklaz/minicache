package main

// TODO move to a separate package

import (
	"container/list"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Cache struct {
	Header  http.Header
	Content []byte

	key         string // cache pool key
	accessCnt   uint
	hash        string // store content hash in deduplicate mode
	index       int    // position in the LRU list
	protectedAt time.Time
}

// implements a min heap,
// less accessed contents go up in the heap and eventually get popped out.
// not using heap.Interface due to personal preference
type lru []*Cache

func (l lru) swap(i, j int) {
	l[i].index, l[j].index = l[j].index, l[i].index
	l[i], l[j] = l[j], l[i]
}

func (l lru) up(c *Cache) {
	id, pid := c.index, c.index>>1 // i*2 and i*2+1 is node i's two children

	if id < 1 || pid < 1 {
		return
	}

	if c.accessCnt < l[pid].accessCnt {
		l.swap(id, pid)
		l.up(l[pid])
	}
}

func (l lru) down(c *Cache) { // TODO don't use recursion
	left := c.index << 1 // is overflow a real concern?
	right := left + 1

	if left >= len(l) || left < 1 {
		return
	}

	// swap with least child
	if c.accessCnt > l[left].accessCnt {
		if right < len(l) && l[right].accessCnt < l[left].accessCnt {
			l.swap(c.index, right)
			l.down(l[right])
		} else {
			l.swap(c.index, left)
			l.down(l[left])
		}
	} else if right < len(l) && c.accessCnt > l[right].accessCnt {
		l.swap(c.index, right)
		l.down(l[right])
	}
}

var (
	cachePool struct {
		pool          map[string]*Cache
		size          int
		mtx           sync.RWMutex
		hashes        map[string]string // store url => content hash in deduplicate mode
		evictorWakeup chan bool
	}

	// fresh requests will go here
	protected struct {
		li  *list.List
		mtx sync.Mutex
	}

	lruContainer struct {
		li  lru
		mtx sync.Mutex
	}
)

func keygen(r *http.Request) string {
	if ConfGlobal.CacheMobile && strings.Contains(r.Header.Get("User-Agent"), "Mobi") {
		return "_" + r.RequestURI
	}
	return r.RequestURI
}

// forwad request to proxy target and create a Cache struct using the response
func NewFromRequest(r *http.Request) (*Cache, *http.Response) {
	fReq, err := http.NewRequest(r.Method, ConfGlobal.TargetAddr+r.RequestURI, nil)
	if err != nil {
		Log("", LOG_ERR)
		return nil, nil
	}

	for h := range r.Header {
		fReq.Header.Add(h, r.Header.Get(h))
	}

	res, err := http.DefaultClient.Do(fReq)
	if err != nil {
		Log("", LOG_ERR)
		return nil, nil
	}
	defer res.Body.Close()

	res.Header.Del("Set-Cookie")
	res.Header.Del("Expires")

	c := &Cache{
		accessCnt: 1,
		key:       keygen(r),
		Header:    res.Header.Clone(),
	}

	c.Content, err = io.ReadAll(res.Body)
	if err != nil {
		Log("", LOG_ERR) // this kind of error handling is C style, probably should use errors.As
	}

	return c, res
}

// move stale cache from protected list to LRU list.
// will lock protected list then lock LRU list
func cacheStale() {
	for {
		time.Sleep(time.Duration(30) * time.Second) // could be configurable, but seems trivial

		protected.mtx.Lock()
		lruContainer.mtx.Lock()

		for c := protected.li.Front(); c != nil &&
			time.Since(c.Value.(*Cache).protectedAt) > ConfGlobal.ProtectionExpire; c = protected.li.Front() {

			cc := protected.li.Remove(c).(*Cache)
			cc.index = len(lruContainer.li) - 1
			lruContainer.li = append(lruContainer.li, cc)
			lruContainer.li.up(lruContainer.li[cc.index])
		}

		protected.mtx.Unlock()
		lruContainer.mtx.Unlock()
	}
}

// remove least recent used cache from pool.
// will lock LRU list then lock cache pool
func lruEvict() {
	for range cachePool.evictorWakeup {
		lruContainer.mtx.Lock()
		cachePool.mtx.Lock()

		for cachePool.size > ConfGlobal.CacheSize && len(lruContainer.li) > 1 {
			c := lruContainer.li[1]

			if ConfGlobal.CacheUnique {
				delete(cachePool.hashes, c.key)
				delete(cachePool.pool, c.hash)
			} else {
				delete(cachePool.pool, c.key)
			}
			cachePool.size -= len(c.Content)

			// pop heap root
			lruContainer.li.swap(1, len(lruContainer.li)-1)
			lruContainer.li = lruContainer.li[:len(lruContainer.li)-1]
			lruContainer.li.down(c)
		}

		cachePool.mtx.Unlock()
		lruContainer.mtx.Unlock()
	}
}

func Init() {
	cachePool.evictorWakeup = make(chan bool)
	cachePool.pool = make(map[string]*Cache)
	if ConfGlobal.CacheUnique {
		cachePool.hashes = make(map[string]string)
	}

	protected.li = list.New()
	lruContainer.li = lru{nil} // index 0 should never be used
	go lruEvict()
	go cacheStale()
}

func get(key string) *Cache {
	var c *Cache

	if ConfGlobal.CacheUnique {
		if hash, ok := cachePool.hashes[key]; ok {
			c = cachePool.pool[hash]
		}
	} else {
		c = cachePool.pool[key]
	}

	return c
}

func Get(r *http.Request) (*Cache, *http.Response) {
	key := keygen(r)

	cachePool.mtx.RLock()
	c := get(key)

	if c == nil {
		cachePool.mtx.Unlock()
		cachePool.mtx.Lock()

		c = get(key) // check again since there's a time window in lock escalation

		if c == nil {
			c, res := NewFromRequest(r)

			if cachePool.size <= ConfGlobal.CacheSize {
				c.protectedAt = time.Now()
				c.index = -1

				protected.mtx.Lock()
				protected.li.PushBack(c)
				protected.mtx.Unlock()

				if ConfGlobal.CacheUnique {
					hash := md5.Sum(c.Content)
					c.hash = hex.EncodeToString(hash[:])
					cachePool.pool[c.hash] = c
					cachePool.hashes[key] = c.hash
				} else {
					cachePool.pool[c.key] = c
				}
				cachePool.size += len(c.Content)
				cachePool.mtx.Unlock()
			} else {
				cachePool.mtx.Unlock()
				go func() { cachePool.evictorWakeup <- true }()

				// if we allowed new cache to be created here,
				// protected list could grow infinitely before anything being moved to LRU list
				Log("New cache can not be added because cache pool is already full.", LOG_INFO)
			}

			return c, res
		}
	}

	c.accessCnt++ // TODO clear this value and move cache back to protected list after certain amount of time
	cachePool.mtx.Unlock()
	return c, nil
}
