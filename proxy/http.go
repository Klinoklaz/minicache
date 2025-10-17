package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/util"
)

func StartHTTPServer() {
	server := &http.Server{
		Addr:         util.Config.LocalAddr,
		Handler:      http.HandlerFunc(mainHandler),
		IdleTimeout:  util.Config.IdleTimeout,
		ReadTimeout:  util.Config.ReadTimeout,
		WriteTimeout: util.Config.WriteTimeout,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig

		util.LogInfo("shutting down server")
		ctx, cancel := context.WithDeadline(
			context.Background(), time.Now().Add(5*time.Second))
		defer cancel()
		server.Shutdown(ctx)
	}()

	util.LogInfo("starting server at %s, targeting %s", util.Config.LocalAddr, util.Config.Target)
	err := server.ListenAndServe()
	if err != http.ErrServerClosed {
		util.LogFatal("failed starting http proxy server. #%s", err)
	}
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithDeadlineCause(r.Context(),
		time.Now().Add(util.Config.TargetTimeout),
		errors.New("target timeout"))
	defer cancel()
	*r = *r.WithContext(ctx)

	// check if non-get requests need to be treated differently
	if r.Method != "GET" && r.Method != "HEAD" {
		switch util.Config.NonGetMode {
		case util.ModePass:
			forward(w, r)
			return
		case util.ModeBlock:
			w.WriteHeader(http.StatusForbidden)
			return
		case util.ModeCache: // no-op
		}
	}

	// bypassing mechanism for auth related request
	if util.Config.AllowAuth &&
		(r.Header.Get("Authorization") != "" ||
			r.Header.Get("Cookie") != "") {
		forward(w, r)
		return
	}

	// a password carried by custom header can be used to force-update the cache
	if util.Config.RefreshHeader != "" &&
		util.Config.RefreshPw != "" &&
		r.Header.Get(util.Config.RefreshHeader) == util.Config.RefreshPw {
		refreshCache(w, r)
	} else {
		getCache(w, r)
	}
}

var directProxy = httputil.ReverseProxy{
	Director: func(r *http.Request) {
		r.URL.Scheme = util.Config.TargetURL.Scheme
		r.URL.Host = util.Config.TargetURL.Host
	},
}

// sends request directly to proxy target, bypass caching
func forward(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			util.LogErr("upstream connection may be broken (forwarding %s) #%v", r.RequestURI, err)
		}
	}()
	if util.RateLimit() {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	util.LogDebug("bypass caching: %s -> %s %s", util.GetRealIP(r), r.Method, r.RequestURI)
	directProxy.ServeHTTP(w, r)
}

var cacheProxy = httputil.ReverseProxy{
	Director: func(r *http.Request) {
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		// avoid caching empty body
		if r.Method == "HEAD" {
			r.Method = "GET"
		}
		r.URL.Scheme = util.Config.TargetURL.Scheme
		r.URL.Host = util.Config.TargetURL.Host
	},
	ModifyResponse: func(res *http.Response) error {
		res.Header.Del("Set-Cookie")
		res.Header.Del("Expires")
		return nil
	},
}

func getCache(w http.ResponseWriter, r *http.Request) {
	key := cache.GetKeyFromRequest(r)
	c, isNew := cache.GetCache(r.Context(), key)
	if !isNew {
		c.WriteResponse(w)
		return
	}

	defer c.HandleProxyPanic(r)
	if util.RateLimit() {
		w.WriteHeader(http.StatusTooManyRequests)
		panic(util.ErrRateLimited) // abort, trigger c.HandleProxyPanic()
	}

	util.LogDebug("cache miss, fetching upstream: %s -> %s", util.GetRealIP(r), key)
	ww := c.WrapResponse(w)
	cacheProxy.ServeHTTP(ww, r)
	ww.LogError()
	cache.FinalizeNewCache(c, key)
}

func refreshCache(w http.ResponseWriter, r *http.Request) {
	key := cache.GetKeyFromRequest(r)
	util.LogDebug("refreshing cache entry: %s", key)

	// the problem with refreshing is, if there is any error,
	// we can not simply remove c from cache pool when it's referenced
	// by protection list or LFU list.
	// so we pass a new one into the proxy, then copy cc's data
	// into c if the whole request is successful
	cc := cache.New(key)
	defer cc.HandleProxyPanic(r)

	ww := cc.WrapResponse(w)
	cacheProxy.ServeHTTP(ww, r)
	ww.LogError()

	c, isNew := cache.GetCache(r.Context(), key)
	if isNew {
		c.CopyFrom(cc)
		cache.FinalizeNewCache(c, key)
	} else {
		c.RefreshFrom(cc)
	}
	util.LogDebug("cache entry updated: %s", c)
}
