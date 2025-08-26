package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/cli"
	"github.com/klinoklaz/minicache/util"
)

const version = "v1.0.0"

func main() {
	var confFile string
	var isCli, showVersion bool
	flag.StringVar(&confFile, "f", "", "Specify a config file")
	flag.BoolVar(&isCli, "c", false, "Interactive cli tool")
	flag.BoolVar(&showVersion, "v", false, "Display version number")
	flag.Parse()
	if showVersion {
		fmt.Println(version)
		return
	}
	if confFile != "" {
		util.LoadConfFile(confFile, isCli)
	}
	if isCli {
		cli.SendCmd()
		return
	}

	go cli.Listen()
	cache.Init()
	server := &http.Server{
		Addr:         util.Config.LocalAddr,
		Handler:      http.HandlerFunc(proxy),
		IdleTimeout:  util.Config.IdleTimeout,
		ReadTimeout:  util.Config.ReadTimeout,
		WriteTimeout: util.Config.WriteTimeout,
	}

	util.Log(util.LogInfo, "starting server at %s, targeting %s", util.Config.LocalAddr, util.Config.TargetAddr)
	err := server.ListenAndServe()
	util.Log(util.LogFatal, "failed starting proxy server. #%s", err)
}

func proxy(w http.ResponseWriter, r *http.Request) {
	// check if non-get requests need to be treated differently
	if r.Method != "GET" {
		switch util.Config.NonGetMode {
		case util.ModePass:
			util.Forward(w, r)
			return
		case util.ModeBlock:
			w.WriteHeader(http.StatusForbidden)
			return
		case util.ModeCache: // no-op
		}
	}
	// bypassing mechanism for auth related requests
	if util.Config.AllowAuth &&
		(r.Header.Get("Authorization") != "" ||
			r.Header.Get("Cookie") != "") {
		util.Forward(w, r)
		return
	}

	var res *http.Response
	var c *cache.Cache
	// a password carried by custom header can be used to force update the cache
	if util.Config.RefreshHeader != "" &&
		util.Config.RefreshPw != "" &&
		r.Header.Get(util.Config.RefreshHeader) == util.Config.RefreshPw {
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
		util.Log(util.LogInfo, "could not send cache, client connection at %s is broken. #%s", r.RemoteAddr, err)
	}
}
