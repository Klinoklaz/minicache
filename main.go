package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/Klinoklaz/minicache/cache"
	"github.com/Klinoklaz/minicache/helper"
)

func main() {
	var confFile string
	flag.StringVar(&confFile, "f", "", "Specify a config file")
	if confFile != "" {
		helper.LoadConfFile(confFile)
	}

	cache.Init()

	mux := http.NewServeMux()
	mux.HandleFunc("/", proxy)
	log.Fatalln(http.ListenAndServe(helper.Config.LocalAddr, mux))
}

func proxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		// TODO:
	}

	c, res := cache.Get(r)

	if c == nil {
		w.WriteHeader(http.StatusBadGateway)
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
		helper.Log("", helper.LOG_ERR)
	}
}
