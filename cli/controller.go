package cli

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/util"
)

const fin byte = '\x00'

// the client side of the cli tool
func SendCmd() {
	conn, err := net.Dial("unix", util.Config.CliSocket)
	if err != nil {
		util.LogErr("failed connecting to cli tool. #%s", err)
		return
	}
	defer conn.Close()

	scn := bufio.NewScanner(os.Stdin)
	monitoring := false
	quit := false
	// used as break and continue
	brk := make(chan bool, 1)
	ctn := make(chan bool, 1)

	fmt.Print("> ")
	for !quit && scn.Scan() {
		_, err := conn.Write(append(scn.Bytes(), ' ')) // never write empty bytes
		if err != nil {
			util.LogErr("failed sending command. #%s", err)
			return
		}
		if monitoring {
			monitoring = false
		} else {
			if t := scn.Text(); t == "monitor" || t == "m" {
				monitoring = true
				ctn <- true // continue accepting input while monitoring
			}
			go getRes(conn, brk, ctn)
		}
		// flow control
		select {
		case <-brk:
			quit = true
		case <-ctn:
		}
	}
	if scn.Err() != nil {
		util.LogErr("invalid cli input. #%s", scn.Err())
	}
}

func getRes(conn net.Conn, brk, ctn chan<- bool) {
	var buf = make([]byte, 2048)
	var err error
	var op string
	var n int
	for {
		if n, err = conn.Read(buf); err != nil {
			op = "getting"
			break
		}
		if _, err = os.Stdout.Write(buf[:n]); err != nil {
			op = "outputting"
			break
		}
		// current cmd done, continue the outer loop
		if buf[n-1] == fin {
			fmt.Print("> ")
			ctn <- true
			return
		}
	}
	if err == io.EOF { // server side connection closed
		fmt.Println("\nbye.")
	} else {
		util.LogErr("failed %s command results. #%s", op, err)
	}
	brk <- true
}

// the server side of the cli tool
func Listen(wg *sync.WaitGroup, exit <-chan os.Signal) {
	if util.Config.CliSocket == "" {
		util.LogInfo("cli tool disabled.")
		return
	}

	os.Remove(util.Config.CliSocket) // error ignored
	l, err := net.Listen("unix", util.Config.CliSocket)
	if err != nil {
		util.LogErr("failed launching cli tool at %s. #%s", util.Config.CliSocket, err)
		return
	}

	// Config.CliSocket can change after reloading,
	// but actual listening socket is always the original one,
	// it's better to pass it as a parameter
	go func(socket string) {
		<-exit
		util.LogInfo("exiting cli tool")
		os.Remove(socket)
		wg.Done()
	}(util.Config.CliSocket)

	// the "monitor" operation takes indefinite time,
	// so there must be a mechanism to abort it
	var monitoring bool
	var oldLevel int
	var oldLog io.Writer

	util.LogInfo("cli tool listening at %s", util.Config.CliSocket)
listen:
	for {
		conn, err := l.Accept()
		if err != nil {
			util.LogErr("cli connection broke. #%s", err)
			continue
		}
		for {
			cmd := getCmd(conn)

			if len(cmd) < 1 {
				continue
			}
			if len(cmd) < 2 {
				cmd = append(cmd, "")
			}
			// abort monitoring and restore logging config if there's any input
			if monitoring {
				util.LogDebug("monitoring end.")
				util.SetLogWriter(oldLog)
				util.Config.LogLevel = oldLevel
				cmd[0] = ""
				monitoring = false
			}

			// process command
			switch cmd[0] {
			case "status", "s":
				_, err = cache.Status(conn)
			case "list", "l":
				_, err = cache.List(conn, cmd[1])
			case "show", "o":
				_, err = cache.Show(conn, cmd[1])
			case "help", "h":
				_, err = help(conn)
			case "reload", "r":
				_, err = reload(conn, cmd[1])
			case "monitor", "m":
				monitoring = true
				oldLevel, oldLog = monitor(conn)
				util.LogDebug("monitoring begin.")
				continue
			case "quit", "q":
				conn.Close()
				continue listen
			}
			// notice client the cmd processing is completed
			if err == nil {
				_, err = conn.Write([]byte{fin})
			}
			if err != nil {
				conn.Close()
				util.LogErr("failed sending command results, %v #%s", cmd, err)
				break
			}
		}
	}
}

// getCmd() doesn't run in separate goroutine
// so it's safe to use globals
var (
	cmdBuf = make([]byte, 2048)
	cmdSep = regexp.MustCompile(`\s+`)
)

func getCmd(conn net.Conn) []string {
	n, err := conn.Read(cmdBuf)
	if err != nil {
		if err != io.EOF {
			util.LogErr("failed getting command. #%s", err)
		}
		return []string{"quit"}
	}
	return cmdSep.Split(strings.TrimSpace(string(cmdBuf[:n])), 2)
}
