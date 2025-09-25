package cache

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"syscall"

	"github.com/klinoklaz/minicache/util"
)

func GetKeyFromRequest(r *http.Request) string {
	prefix := ""
	if util.Config.NonGetMode == util.ModeCache {
		method := r.Method
		if method == "HEAD" {
			method = "GET"
		}
		prefix += method + "_"
	}
	if util.Config.CacheMobile && strings.Contains(r.Header.Get("User-Agent"), "Mobi") {
		prefix = "_" + prefix
	}
	return prefix + r.RequestURI
}

// wrap http.ResponseWriter for copying data into cache
type ResponseWrapper struct {
	w   http.ResponseWriter
	c   *Cache
	err error
}

// implement http.ResponseWriter
func (rw *ResponseWrapper) Header() http.Header {
	return rw.w.Header()
}

// implement http.ResponseWriter
func (rw *ResponseWrapper) Write(data []byte) (int, error) {
	rw.c.Body = slices.Concat(rw.c.Body, data)
	// suppress the orginal error to ensure response body
	// can be fully copied into cache during multiple Write() calls
	if rw.err == nil {
		_, rw.err = rw.w.Write(data)
	}
	return len(data), nil
}

// implement http.ResponseWriter
func (rw *ResponseWrapper) WriteHeader(statusCode int) {
	rw.w.WriteHeader(statusCode)
	if !slices.Contains(util.Config.CacheStatus, statusCode) {
		rw.c.status = invalid
		util.LogDebug("status not cacheable (%d): %s", statusCode, rw.c.keys[0])
	}
	rw.c.StatusCode = statusCode
	rw.c.Header = rw.w.Header().Clone()
}

func (rw *ResponseWrapper) LogError() {
	if rw.err != nil &&
		!errors.Is(rw.err, syscall.EPIPE) &&
		!errors.Is(rw.err, syscall.ECONNRESET) {
		util.LogInfo("failed writing response when creating cache: %s #%s", rw.c.keys[0], rw.err)
	}
}

func (c *Cache) WrapResponse(w http.ResponseWriter) *ResponseWrapper {
	return &ResponseWrapper{w: w, c: c}
}

func (c *Cache) WriteResponse(w http.ResponseWriter) {
	for h := range c.Header {
		w.Header().Set(h, c.Header.Get(h))
	}
	w.WriteHeader(c.StatusCode)

	_, err := w.Write(c.Body)
	// write errors are mostly client error, not very important
	if err != nil &&
		!errors.Is(err, syscall.EPIPE) &&
		!errors.Is(err, syscall.ECONNRESET) {
		util.LogInfo("failed writing response, cache key: %s #%s", c.keys[0], err)
	}
}

// deal with panic thrown by httputil.ReverseProxy.ServeHTTP().
// this basically does the same thing as setting c.status = invalid
// then pass it into FinalizeNewCache()
func (c *Cache) HandleProxyPanic() {
	err := recover()
	if err == nil {
		return
	}

	cachePool.mtx.Lock()
	delete(cachePool.pool, c.keys[0])
	cachePool.mtx.Unlock()

	close(c.ready)
	util.LogErr("upstream connection may be broken, was trying to fetch %s #%v", c.keys[0], err)
}
