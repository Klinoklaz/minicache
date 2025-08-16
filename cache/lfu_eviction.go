package cache

import (
	"container/list"
	"slices"
	"sync"
	"time"

	"github.com/klinoklaz/minicache/util"
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
func (p *protecting) unprotect(condition func(*Cache) bool) {
	for c := p.li.Front(); c != nil && condition(c.Value.(*Cache)); c = p.li.Front() {
		cc := p.li.Remove(c).(*Cache)
		cc.status = stale
		lfuList.li = append(lfuList.li, cc)
		util.Log(util.LogDebug, "moving protected cache entry to LFU list. %s", cc)
	}
}

// remove least frequently used cache entry from pool
func lfuEvict() {
	// shrink the pool size to 3/4 capacity in one go
	// to prevent constant triggering of eviction
	goal := util.Config.CacheSize * 3 / 4

	for range cachePool.evictorWakeup {
		cachePool.mtx.Lock()
		protectList.mtx.Lock()
		lfuList.mtx.Lock()

		// evction won't work if we don't have enough entries in LFU list.
		// force a dequeue quota on protected list
		// to guarantee at least this much of cache will be evicted
		evictionQuota := cachePool.size - goal
		protectList.unprotect(func(c *Cache) bool {
			forceStale := evictionQuota > 0
			evictionQuota -= len(c.Content)
			return forceStale || time.Since(c.protectedAt) > util.Config.ProtectionExpire
		})

		// sort in access count desc, content length asc
		slices.SortFunc(lfuList.li, func(a, b *Cache) int {
			return b.accessCnt - a.accessCnt + len(a.Content) - len(b.Content)
		})

		for cachePool.size > goal && len(lfuList.li) > 0 {
			c := lfuList.li[len(lfuList.li)-1]
			// check if c was reprotected
			if c.status == protect {
				continue
			}

			for _, key := range c.keys {
				delete(cachePool.pool, key)
			}
			if util.Config.CacheUnique {
				delete(cachePool.hashes, c.hash)
			}
			cachePool.size -= len(c.Content)
			lfuList.li = lfuList.li[:len(lfuList.li)-1]

			util.Log(util.LogDebug, "evicting cache entry. %s", c)
		}

		cachePool.mtx.Unlock()
		protectList.mtx.Unlock()
		lfuList.mtx.Unlock()
	}
}
