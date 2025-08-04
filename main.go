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

	c, res := cache.Get(r)

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
