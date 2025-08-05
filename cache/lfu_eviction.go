package cache

import (
	"container/list"
	"slices"
	"sync"
	"time"

	"github.com/klinoklaz/minicache/helper"
)

type protecting struct {
	li  *list.List
	mtx sync.Mutex
}

type evicting struct {
	li  []*Cache
	mtx sync.Mutex
}

// protect cache entry from LFU eviction.
// don't put this inside cache pool mutex's critical area
func (p *protecting) protect(c *Cache) {
	c.protectedAt = time.Now()
	c.status = protect

	p.mtx.Lock()
	p.li.PushBack(c)
	p.mtx.Unlock()
}

var (
	// protected list, fresh cache entries go here.
	protectList protecting = protecting{li: list.New()}

	lfuList evicting

	// stale cache entry goes from LFU list to here
	// if it wasn't evicted after some configured time
	reprotectList protecting = protecting{li: list.New()}
)

// move stale cache from protection to LFU list.
func (p *protecting) unprotect() {
	p.mtx.Lock()
	lfuList.mtx.Lock()

	for c := p.li.Front(); c != nil &&
		time.Since(c.Value.(*Cache).protectedAt) > helper.Config.ProtectionExpire; c = p.li.Front() {

		cc := p.li.Remove(c).(*Cache)
		cc.status = stale
		lfuList.li = append(lfuList.li, cc)

		helper.Log(helper.LogDebug, "moving protected cache entry to LFU list. %s", cc)
	}

	p.mtx.Unlock()
	lfuList.mtx.Unlock()
}

func cacheStale() {
	for {
		time.Sleep(30 * time.Second) // could be configurable, but seems trivial
		protectList.unprotect()
		reprotectList.unprotect()
	}
}

// remove least recent used cache entry from pool
func lfuEvict() {
	for range cachePool.evictorWakeup {
		cachePool.mtx.Lock()
		lfuList.mtx.Lock()

		// sort in desc
		slices.SortFunc(lfuList.li, func(a, b *Cache) int {
			return b.accessCnt - a.accessCnt
		})

		// delete last
		for cachePool.size > helper.Config.CacheSize && len(lfuList.li) > 0 {
			c := lfuList.li[len(lfuList.li)-1]

			for _, key := range c.keys {
				delete(cachePool.pool, key)
			}
			if helper.Config.CacheUnique {
				delete(cachePool.hashes, c.hash)
			}
			cachePool.size -= len(c.Content)
			lfuList.li = lfuList.li[:len(lfuList.li)-1]

			helper.Log(helper.LogDebug, "evicting cache entry. %s", c)
		}

		cachePool.mtx.Unlock()
		lfuList.mtx.Unlock()
	}
}
