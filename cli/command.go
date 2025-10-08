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
	err := util.LoadConfFile(newFile, false)
	cache.Unblock()

	if err != nil {
		util.LogErr("failed loading config file. #%s", err)
		return fmt.Fprintf(conn, "nothing changed: %+v\n", util.Config)
	}
	return fmt.Fprintf(conn, "reload ok, current configurations: %+v\n", util.Config)
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
		"\tlist, l [a|f|s [<num>]]\tlist up to <num> cache entries, sorted in\n"+
		"\t\t\t\t  access|access frequency|size descending\n"+
		"\t\t\t\t  order, or ascending order for negative\n"+
		"\t\t\t\t  <num>. default behavior is `list f 20`\n"+
		"\tstatus, s\t\tdisplay status info about the cache pool\n"+
		"\tshow, o <key>\t\tdisplay details about specified cache entry\n"+
		"\thelp, h\t\t\tdisplay this help message\n"+
		"\tmonitor, m\t\ttrack debug log in real time\n"+
		"\treload, r [<file>]\treload config file for the caching proxy\n"+
		"\tquit, q\t\t\texit the interactive cli tool\n")
}
