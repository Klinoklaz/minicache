package cache

import (
	"container/list"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/klinoklaz/minicache/util"
)

type Cache struct {
	Header     http.Header
	Body       []byte
	StatusCode int

	ready     chan bool
	keys      []string // cache pool key
	accessCnt int
	hash      [16]byte
	status    byte
	cntBegin  time.Time
}

func (c *Cache) String() string {
	return fmt.Sprintf("URIs: %v, access count: %d, status: %c, content length: %d, hash: %s, counting began: %s",
		c.keys,
		c.accessCnt,
		c.status,
		len(c.Body),
		hex.EncodeToString(c.hash[:]),
		c.cntBegin.Format(time.StampMicro))
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
	mtx           sync.RWMutex        // see lfuEvict() for lock order
	hashes        map[[16]byte]*Cache // stores content md5 sum in deduplicate mode
	evictorWakeup chan bool
}

// can not rely on package init cuz we have to wait for config to be loaded
func Init() {
	cachePool.evictorWakeup = make(chan bool)
	cachePool.pool = make(map[string]*Cache)
	if util.Config.CacheUnique {
		cachePool.hashes = make(map[[16]byte]*Cache)
	}

	protectList.li = list.New()
	go lfuEvict()
}

func (c *Cache) RefreshFrom(cc *Cache) {
	if cc.status == invalid {
		return
	}

	cachePool.mtx.Lock()
	defer cachePool.mtx.Unlock()

	cachePool.size += len(cc.Body) - len(c.Body)
	c.Body = cc.Body
	c.Header = cc.Header.Clone()
	c.StatusCode = cc.StatusCode
	// c.hash doesn't need to be updated
}

// shallow copy
func (c *Cache) CopyFrom(cc *Cache) {
	r := c.ready
	*c = *cc
	c.ready = r
}

func New(key string) *Cache {
	return &Cache{
		ready:      make(chan bool),
		keys:       []string{key},
		accessCnt:  1,
		status:     fresh,
		StatusCode: http.StatusOK,
		cntBegin:   time.Now(),
	}
}

// retrieve cache entry from pool, or create a new one with empty data
func GetCache(ctx context.Context, key string) (c *Cache, isNew bool) {
	cachePool.mtx.RLock()
	c = cachePool.pool[key]

	// can't `&& c.status != invalid` here
	// because it causes concurrent retry, which does no benefit
	if c != nil {
		cachePool.mtx.RUnlock()
		countAccess(c, ctx)
		return c, false
	}

	cachePool.mtx.RUnlock()
	cachePool.mtx.Lock()

	// check again since there's a time window in lock upgrade
	c = cachePool.pool[key]
	// already created by other concurrent reqest
	if c != nil {
		cachePool.mtx.Unlock()
		countAccess(c, ctx)
		return c, false
	}
	// first request
	c = New(key)
	cachePool.pool[key] = c
	cachePool.mtx.Unlock()

	return c, true
}

// do some post-processing according to c's final state
func FinalizeNewCache(c *Cache, key string) {
	defer close(c.ready)

	if c.status == invalid {
		cachePool.mtx.Lock()
		delete(cachePool.pool, key)
		cachePool.mtx.Unlock()
		return
	}

	cachePool.mtx.Lock()
	util.LogDebug("adding new cache entry: %s (%d, %d B)", c.keys[0], c.StatusCode, len(c.Body))

	if cachePool.size += len(c.Body); cachePool.size > util.Config.CacheSize {
		util.LogDebug("cache pool size limit reached, currently %d, try to start evicting.", cachePool.size)
		go func() { cachePool.evictorWakeup <- true }()
	}

	if !util.Config.CacheUnique {
		cachePool.mtx.Unlock()
		protectList.protect(c)
		return
	}

	c.hash = md5.Sum(c.Body)
	// check hashes to ensure same response data being added only once to the pool
	if cc, ok := cachePool.hashes[c.hash]; ok {
		cachePool.pool[c.keys[0]] = cc
		cc.keys = append(cc.keys, c.keys[0])
		cachePool.size -= len(c.Body)
		cachePool.mtx.Unlock()
		util.LogDebug("found duplicated content for %s, merge into existing one. %s", c.keys[0], cc)
	} else {
		cachePool.hashes[c.hash] = c
		cachePool.mtx.Unlock()
		protectList.protect(c)
	}
}

// don't put this inside cache pool's mutex section,
// or it will create dead locks with FinalizeNewEntry()
func countAccess(c *Cache, ctx context.Context) {
	select {
	case <-c.ready:
	case <-ctx.Done():
		// have wrote several deadlocks around this logic,
		// better add a warning
		util.LogWarn("timeout while waiting for cache finalizing, key: %s", c.keys[0])
		return
	}

	if c.status == invalid {
		return
	}

	// access count doesn't need to be accurate, so no locking on individual entry
	if time.Since(c.cntBegin) <= util.Config.LfuTime {
		c.accessCnt++
		return
	}

	c.accessCnt = 1 // restart counting
	c.cntBegin = time.Now()
	// NOTE: race, potentially can cause duplicated entries in the list,
	// but kinda ok
	if c.status != protect {
		protectList.protect(c)
		util.LogDebug("reprotect cache entry: %s", c)
	}
	// there's no way to remove c from LFU list after reprotecting it
	// since we don't know its index, this also leads to duplicated entries in LFU list.
	// the evictor function must take care of this situation
}
