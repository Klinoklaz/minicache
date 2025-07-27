package cache

import (
	"container/list"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Klinoklaz/minicache/helper"
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

var cachePool struct {
	pool          map[string]*Cache
	size          int
	mtx           sync.RWMutex      // must be locked before protected list and LRU list
	hashes        map[string]string // store url => content hash in deduplicate mode
	evictorWakeup chan bool
}

// can not rely on package init cuz we have to wait for config to be loaded
func Init() {
	cachePool.evictorWakeup = make(chan bool)
	cachePool.pool = make(map[string]*Cache)
	if helper.Config.CacheUnique {
		cachePool.hashes = make(map[string]string)
	}

	protectList.li = list.New()
	lruList.li = lru{nil} // index 0 should never be used
	go lruEvict()
	go cacheStale()
}

func keygen(r *http.Request) string {
	if helper.Config.CacheMobile && strings.Contains(r.Header.Get("User-Agent"), "Mobi") {
		return "_" + r.RequestURI
	}
	return r.RequestURI
}

// forwad request to proxy target and create a Cache struct using the response
func NewFromRequest(r *http.Request) (*Cache, *http.Response) {
	fReq, err := http.NewRequest(r.Method, helper.Config.TargetAddr+r.RequestURI, nil)
	if err != nil {
		helper.Log("", helper.LOG_ERR)
		return nil, nil
	}

	for h := range r.Header {
		fReq.Header.Add(h, r.Header.Get(h))
	}

	res, err := http.DefaultClient.Do(fReq)
	if err != nil {
		helper.Log("", helper.LOG_ERR)
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
		helper.Log("", helper.LOG_ERR) // this kind of error handling is C style, probably should use errors.As
	}

	return c, res
}

func get(key string) *Cache {
	var c *Cache

	if helper.Config.CacheUnique {
		if hash, ok := cachePool.hashes[key]; ok {
			c = cachePool.pool[hash]
		}
	} else {
		c = cachePool.pool[key]
	}

	return c
}

// get cache entry
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

			if res.StatusCode != http.StatusOK {
				cachePool.mtx.Unlock()
			} else if cachePool.size <= helper.Config.CacheSize {
				protect(c)

				if helper.Config.CacheUnique {
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
				helper.Log("New cache can not be added because cache pool is already full.", helper.LOG_NOTICE)
			}

			return c, res
		}
	}

	// track access count
	if time.Since(c.protectedAt) > helper.Config.LruTime { // restart tracking
		id := c.index // FIXME: race
		if id != PROTECT {
			protect(c)
		}

		lruList.mtx.Lock()
		c.accessCnt = 1

		if id != PROTECT {
			// rearrange LRU list since the entry was moved back to protection
			lruList.li.swap(id, len(lruList.li)-1)
			lruList.li = lruList.li[:len(lruList.li)-1]
			lruList.li.fix(id)
		}
		lruList.mtx.Unlock()
	} else {
		lruList.mtx.Lock()
		c.accessCnt++
		if c.index != PROTECT {
			lruList.li.down(c)
		}
		lruList.mtx.Unlock()
	}

	cachePool.mtx.Unlock()
	return c, nil
}
