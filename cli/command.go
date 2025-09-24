package cli

import (
	"fmt"
	"io"
	"net"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/util"
)

// reloads config file
func reload(conn net.Conn, newFile string) (int, error) {
	if newFile == "" {
		newFile = util.LastConfFile
	}
	cache.Block()
	util.LoadConfFile(newFile, false)
	cache.Unblock()
	return fmt.Fprintf(conn, "config file loaded, current conf values: %+v\n", util.Config)
}

// prints debug log to cli tool, not concurrency-safe
func monitor(conn net.Conn) (int, io.Writer) {
	oldLevel := util.Config.LogLevel
	util.Config.LogLevel = util.LogLevelDebug
	oldLog := util.GetLogWriter()
	util.SetLogWriter(io.MultiWriter(oldLog, conn))
	return oldLevel, oldLog
}

// prints a list of usable commands
func help(conn net.Conn) (int, error) {
	return fmt.Fprint(conn, "Commands:\n"+
		"\tstatus, s\t\tdisplay status info about the cache pool\n"+
		"\tlist, l\t\t\tdisplay all cache keys\n"+
		"\tshow, o <key>\t\tdisplay details about specified cache entry\n"+
		"\thelp, h\t\t\tdisplay this help message\n"+
		"\tmonitor, m\t\ttrack debug log in real time\n"+
		"\treload, r <file>\treload config file for the caching proxy\n"+
		"\tquit, q\t\t\texit the interactive cli tool\n")
}
