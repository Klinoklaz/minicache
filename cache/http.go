package cache

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"syscall"

	"github.com/klinoklaz/minicache/util"
)

func GetKeyFromReqest(r *http.Request) string {
	prefix := ""
	if util.Config.NonGetMode == util.ModeCache {
		prefix += r.Method + "_"
	}
	if util.Config.CacheMobile && strings.Contains(r.Header.Get("User-Agent"), "Mobi") {
		prefix = "_" + prefix
	}
	return prefix + r.RequestURI
}

// this doesn't comply with definitions of "non-cacheable" in http RFCs,
// only includes volatile statuses or those can cause errors if were cached
/* var nonCacheableStatus = []int{
	http.StatusContinue,
	http.StatusSwitchingProtocols,
	http.StatusProcessing,
	http.StatusEarlyHints,
	http.StatusCreated,
	http.StatusAccepted,
	http.StatusRequestTimeout,
	http.StatusConflict,
	http.StatusLocked,
	http.StatusTooEarly,
	http.StatusTooManyRequests,
	http.StatusBadGateway,
	http.StatusGatewayTimeout,
}*/

// wrap http.ResponseWriter for copying data into cache
type ResponseWrapper struct {
	w   http.ResponseWriter
	c   *Cache
	err error
}

func (rw *ResponseWrapper) Header() http.Header {
	return rw.w.Header()
}

func (rw *ResponseWrapper) Write(data []byte) (int, error) {
	rw.c.Body = slices.Concat(rw.c.Body, data)
	// suppress the orginal error to ensure response body
	// can be fully copied into cache during multiple Write() calls
	if rw.err == nil {
		_, rw.err = rw.w.Write(data)
	}
	return len(data), nil
}

func (rw *ResponseWrapper) WriteHeader(statusCode int) {
	rw.w.WriteHeader(statusCode)
	if statusCode != http.StatusOK {
		rw.c.status = invalid
	}
	rw.c.StatusCode = statusCode
	rw.c.Header = rw.w.Header().Clone()
}

func (c *Cache) WrapResponse(w http.ResponseWriter) *ResponseWrapper {
	return &ResponseWrapper{w: w, c: c}
}

func (c *Cache) WriteResponse(w http.ResponseWriter) {
	for h := range c.Header {
		w.Header().Add(h, c.Header.Get(h))
	}
	w.WriteHeader(c.StatusCode)

	_, err := w.Write(c.Body)
	if err != nil && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, syscall.ECONNRESET) {
		util.Log(util.LogInfo, "failed writing response, cache key: %s #%s", c.keys[0], err)
	}
}
