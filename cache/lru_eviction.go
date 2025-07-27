package cache

import (
	"container/list"
	"sync"
	"time"

	"github.com/Klinoklaz/minicache/helper"
)

// implements a min heap,
// less accessed contents go up in the heap and eventually get popped out.
// not using heap.Interface due to personal preference
type lru []*Cache

func (l lru) swap(i, j int) {
	l[i].index, l[j].index = l[j].index, l[i].index
	l[i], l[j] = l[j], l[i]
}

func (l lru) up(c *Cache) {
	id, pid := c.index, c.index>>1 // i*2 and i*2+1 are node i's two children

	for id > 0 && pid > 0 && c.accessCnt < l[pid].accessCnt {
		l.swap(id, pid)
		id, pid = pid, pid>>1
	}
}

func (l lru) down(c *Cache) {
	left := c.index << 1 // is overflow a real concern?
	right := left + 1

	for left < len(l) && left > 0 {
		// swap with least child
		if c.accessCnt > l[left].accessCnt {
			if right < len(l) && l[right].accessCnt < l[left].accessCnt {
				l.swap(c.index, right)
				left = right << 1
				right = left + 1
			} else {
				l.swap(c.index, left)
				left = left << 1
				right = left + 1
			}
		} else if right < len(l) && c.accessCnt > l[right].accessCnt {
			l.swap(c.index, right)
			left = right << 1
			right = left + 1
		} else {
			break
		}
	}
}

func (l lru) fix(i int) {
	pi := i >> 1 // parent

	if i < 1 || i >= len(l) {
		return
	}

	if pi > 0 && l[i].accessCnt < l[pi].accessCnt {
		l.up(l[i])
	} else {
		l.down(l[i])
	}
}

var (
	// protected list, fresh requests will go here.
	// must be locked after cache pool and before LRU list
	protectList struct {
		li  *list.List
		mtx sync.Mutex
	}

	// LRU list.
	// must be locked after cache pool and protected list
	lruList struct {
		li  lru
		mtx sync.Mutex
	}
)

// add cache entry to proteced list
func protect(c *Cache) {
	c.protectedAt = time.Now()
	c.index = -1

	protectList.mtx.Lock()
	protectList.li.PushBack(c)
	protectList.mtx.Unlock()
}

// move stale cache from protected list to LRU list.
func cacheStale() {
	for {
		time.Sleep(time.Duration(30) * time.Second) // could be configurable, but seems trivial

		protectList.mtx.Lock()
		lruList.mtx.Lock()

		for c := protectList.li.Front(); c != nil &&
			time.Since(c.Value.(*Cache).protectedAt) > helper.Config.ProtectionExpire; c = protectList.li.Front() {

			cc := protectList.li.Remove(c).(*Cache)
			cc.index = len(lruList.li) - 1
			lruList.li = append(lruList.li, cc)
			lruList.li.up(lruList.li[cc.index])
		}

		protectList.mtx.Unlock()
		lruList.mtx.Unlock()
	}
}

// remove least recent used cache entry from pool
func lruEvict() {
	for range cachePool.evictorWakeup {
		cachePool.mtx.Lock()
		lruList.mtx.Lock()

		for cachePool.size > helper.Config.CacheSize && len(lruList.li) > 1 {
			c := lruList.li[1]

			if helper.Config.CacheUnique {
				delete(cachePool.hashes, c.key)
				delete(cachePool.pool, c.hash)
			} else {
				delete(cachePool.pool, c.key)
			}
			cachePool.size -= len(c.Content)

			// pop heap root
			lruList.li.swap(1, len(lruList.li)-1)
			lruList.li = lruList.li[:len(lruList.li)-1]
			lruList.li.down(c)
		}

		cachePool.mtx.Unlock()
		lruList.mtx.Unlock()
	}
}
