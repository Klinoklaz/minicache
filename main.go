package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/cli"
	"github.com/klinoklaz/minicache/proxy"
	"github.com/klinoklaz/minicache/util"
)

const version = "v1.2.1" // TODO: probably should use ldflags

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

	cache.Init()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-shutdown
		signal.Stop(shutdown)
		close(shutdown) // broadcast shutdown event
	}()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go cli.Listen(wg, shutdown)
	proxy.StartHTTPServer(shutdown)

	wg.Wait()
}
