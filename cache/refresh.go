package cache

import (
	"net/http"
)

// cache refresh function for the use inside http handler
func Refresh(r *http.Request) (*Cache, *http.Response) {
	key := keygen(r)

	// create a new variable to make request.
	// since the original cache entry is referenced everywhere,
	// refreshing it directly would be a huge pain
	cc := &Cache{ready: make(chan bool), keys: []string{key}}
	res := cc.newRequest(r)
	if cc.status == invalid {
		close(cc.ready)
		return cc, res
	}

	cachePool.mtx.Lock()

	c, ok := cachePool.pool[key]
	if !ok {
		cachePool.pool[key] = cc
		cachePool.mtx.Unlock()

		accept(cc)
		close(cc.ready)
		return cc, res
	}

	// wait for any other updates to complete, then make others wait
	<-c.ready
	c.ready = make(chan bool)
	// if old entry is already accepted, resize the pool.
	// this condition is safe cuz cache evicting locks the pool,
	// and ready channel blocks until the entry gets protected
	if c.status == protect || c.status == stale {
		cachePool.size += len(cc.Content) - len(c.Content)
	}
	cachePool.mtx.Unlock()
	// only Header and Content fields of the original cache entry will be refreshed
	// in order to avoid the trouble of updating its references (i.e. cache pool hashes)
	c.Content = cc.Content
	c.Header = cc.Header.Clone()

	close(c.ready)

	return c, res
}
