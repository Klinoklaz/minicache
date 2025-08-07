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
// don't put this inside cache pool's mutex section
func (p *protecting) protect(c *Cache) {
	c.protectedAt = time.Now()
	c.status = protect

	p.mtx.Lock()
	p.li.PushBack(c)
	p.mtx.Unlock()
}

var (
	// protected list, fresh cache entries go here.
	// stale cache entries go from LFU list to here as well,
	// if it wasn't evicted after some configured time
	protectList protecting = protecting{li: list.New()}

	lfuList evicting
)

// move cache entries from protection to LFU list by condition.
func (p *protecting) unprotect(condition func(*list.Element) bool) {
	for c := p.li.Front(); c != nil && condition(c); c = p.li.Front() {

		cc := p.li.Remove(c).(*Cache)
		cc.status = stale
		lfuList.li = append(lfuList.li, cc)

		helper.Log(helper.LogDebug, "moving protected cache entry to LFU list. %s", cc)
	}
}

func cacheStale() {
	for {
		time.Sleep(30 * time.Second) // could be configurable, but seems trivial
		protectList.mtx.Lock()
		lfuList.mtx.Lock()

		protectList.unprotect(func(e *list.Element) bool {
			return time.Since(e.Value.(*Cache).protectedAt) > helper.Config.ProtectionExpire
		})

		protectList.mtx.Unlock()
		lfuList.mtx.Unlock()
	}
}

// remove least frequently used cache entry from pool
func lfuEvict() {
	for range cachePool.evictorWakeup {
		cachePool.mtx.Lock()
		protectList.mtx.Lock()
		lfuList.mtx.Lock()

		// evction won't work if we don't have enough entries in LFU list.
		// force a dequeue quota on protected list
		// to guarantee at least this much of cache will be evicted
		evictionQuota := cachePool.size - helper.Config.CacheSize
		protectList.unprotect(func(e *list.Element) bool {
			forceStale := evictionQuota > 0
			evictionQuota -= len(e.Value.(*Cache).Content)
			return forceStale
		})

		// sort in access count desc, content length asc
		slices.SortFunc(lfuList.li, func(a, b *Cache) int {
			return b.accessCnt - a.accessCnt + len(a.Content) - len(b.Content)
		})

		// delete last
		for cachePool.size > helper.Config.CacheSize && len(lfuList.li) > 0 {
			c := lfuList.li[len(lfuList.li)-1]
			// check if c was reprotected
			if c.status == protect {
				continue
			}

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
		protectList.mtx.Unlock()
		lfuList.mtx.Unlock()
	}
}
