package main

import (
	"flag"
	"net/http"

	"github.com/Klinoklaz/minicache/cache"
	"github.com/Klinoklaz/minicache/helper"
)

func main() {
	var confFile string
	flag.StringVar(&confFile, "f", "", "Specify a config file")
	flag.Parse()
	if confFile != "" {
		helper.LoadConfFile(confFile)
	}

	cache.Init()

	mux := http.NewServeMux()
	mux.HandleFunc("/", proxy)

	helper.Log(helper.LogInfo, "starting server at %s, targeting %s", helper.Config.LocalAddr, helper.Config.TargetAddr)
	err := http.ListenAndServe(helper.Config.LocalAddr, mux)
	helper.Log(helper.LogFatal, "failed starting proxy server. #%s", err)
}

func proxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		switch helper.Config.NonGetMode {
		case helper.ModePass:
			helper.Forward(w, r)
			return
		case helper.ModeBlock:
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
