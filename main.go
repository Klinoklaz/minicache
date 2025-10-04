package main

import (
	"flag"
	"fmt"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/cli"
	"github.com/klinoklaz/minicache/proxy"
	"github.com/klinoklaz/minicache/util"
)

const version = "v1.1.7" // TODO: probably should use ldflags

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

	if confFile == "" {
		util.LogFatal("no config file provided, exiting")
	} else if err := util.LoadConfFile(confFile, isCli); err != nil {
		util.LogWarn("failed loading config file, using default values. #%s", err)
	}

	if isCli {
		cli.SendCmd()
		return
	}

	go cli.Listen()
	cache.Init()
	proxy.StartHTTPServer()
}
