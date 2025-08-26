package main

import (
	"flag"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/cli"
	"github.com/klinoklaz/minicache/proxy"
	"github.com/klinoklaz/minicache/util"
)

func main() {
	var confFile string
	var isCli bool
	flag.StringVar(&confFile, "f", "", "Specify a config file")
	flag.BoolVar(&isCli, "c", false, "Interactive cli tool")
	flag.Parse()
	if confFile != "" {
		util.LoadConfFile(confFile, isCli)
	}
	if isCli {
		cli.SendCmd()
		return
	}

	go cli.Listen()
	cache.Init()
	proxy.StartHTTPServer()
}
