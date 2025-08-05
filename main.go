package main

import (
	"flag"
	"net/http"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/helper"
)

func main() {
	var confFile string
	flag.StringVar(&confFile, "f", "", "Specify a config file")
	flag.Parse()
	if confFile != "" {
		helper.LoadConfFile(confFile)
	}

	helper.LogSignal()
	cache.Init()

	server := &http.Server{
		Addr:         helper.Config.LocalAddr,
		Handler:      http.HandlerFunc(proxy),
		IdleTimeout:  helper.Config.IdleTimeout,
		ReadTimeout:  helper.Config.ReadTimeout,
		WriteTimeout: helper.Config.WriteTimeout,
	}

	helper.Log(helper.LogInfo, "starting server at %s, targeting %s", helper.Config.LocalAddr, helper.Config.TargetAddr)
	err := server.ListenAndServe()
	helper.Log(helper.LogFatal, "failed starting proxy server. #%s", err)
}

func proxy(w http.ResponseWriter, r *http.Request) {
	// check if non-get requests need to be treated differently
	if r.Method != "GET" {
		switch helper.Config.NonGetMode {
		case helper.ModePass:
			helper.Forward(w, r)
			return
		case helper.ModeBlock:
			w.WriteHeader(http.StatusForbidden)
			return
		case helper.ModeQueue:
			helper.Queue(w, r)
			return
		case helper.ModeCache: // no-op
		}
	}

	var res *http.Response
	var c *cache.Cache
	// a password carried by custom header can be used to force update the cache
	if helper.Config.RefreshHeader != "" &&
		helper.Config.RefreshPw != "" &&
		r.Header.Get(helper.Config.RefreshHeader) == helper.Config.RefreshPw {
		c, res = cache.Refresh(r)
	} else {
		c, res = cache.Get(r)
	}
	// since the Get and Refresh function handles creation of new cache,
	// nothing else needs to be done here
	if c == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	status := http.StatusOK
	if res != nil {
		status = res.StatusCode
	}

	for h := range c.Header {
		w.Header().Add(h, c.Header.Get(h))
	}
	w.WriteHeader(status)

	_, err := w.Write(c.Content)
	if err != nil {
		helper.Log(helper.LogInfo, "could not send cache, client connection at %s is broken. #%s", r.RemoteAddr, err)
	}
}
