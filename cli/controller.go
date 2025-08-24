package cli

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"

	"github.com/klinoklaz/minicache/cache"
	"github.com/klinoklaz/minicache/util"
)

var (
	srvBuf = make([]byte, 2048) // server side read
	cliBuf = make([]byte, 2048) // client side read
	cmdSep = regexp.MustCompile(`\s+`)
)

// the client side of the cli tool
func SendCmd() {
	conn, err := net.Dial("unix", util.Config.CliSocket)
	if err != nil {
		util.Log(util.LogErr, "failed connecting to cli tool. #%s", err)
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
			util.Log(util.LogErr, "failed sending command. #%s", err)
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
		util.Log(util.LogErr, "invalid cli input. #%s", scn.Err())
	}
}

func getRes(conn net.Conn, brk, ctn chan<- bool) {
	for {
		n, err := conn.Read(cliBuf)
		if err != nil {
			// normally server side connection is closed first
			if err == io.EOF {
				fmt.Println("\nbye.")
			} else {
				util.Log(util.LogErr, "failed getting command results. #%s", err)
			}
			brk <- true
			return
		}
		_, err = os.Stdout.Write(cliBuf[:n])
		if err != nil {
			util.Log(util.LogErr, "failed outputting command results. #%s", err)
			brk <- true
			return
		}
		// current cmd done, continue the outer loop
		if cliBuf[n-1] == '\x00' {
			fmt.Print("> ")
			ctn <- true
			return
		}
	}
}

// the server side of the cli tool
func Listen() {
	if util.Config.CliSocket == "" {
		util.Log(util.LogInfo, "cli tool disabled.")
		return
	}

	os.Remove(util.Config.CliSocket) // error ignored
	l, err := net.Listen("unix", util.Config.CliSocket)
	if err != nil {
		util.Log(util.LogErr, "failed launching cli tool at %s. #%s", util.Config.CliSocket, err)
		return
	}

	// the "monitor" operation takes indefinite time,
	// so there must be a mechanism to abort it
	var monitoring bool
	var oldLevel int
	var oldLog io.Writer

listen:
	for {
		conn, err := l.Accept()
		if err != nil {
			util.Log(util.LogErr, "cli connection broke. #%s", err)
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
				util.Log(util.LogDebug, "monitoring end.")
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
				_, err = cache.List(conn)
			case "show", "o":
				_, err = cache.Show(conn, cmd[1])
			case "help", "h":
				_, err = help(conn)
			case "reload", "r":
				_, err = reload(conn, cmd[1])
			case "monitor", "m":
				monitoring = true
				oldLevel, oldLog = monitor(conn)
				util.Log(util.LogDebug, "monitoring begin.")
				continue
			case "quit", "q":
				conn.Close()
				continue listen
			}

			if err != nil {
				util.Log(util.LogErr, "failed sending command results. #%s", err)
				conn.Close()
				break
			}
			// notice client the cmd processing is completed
			_, err = conn.Write([]byte{'\x00'})
			if err != nil {
				util.Log(util.LogErr, "failed sending the zero byte. #%s", err)
				conn.Close()
				break
			}
		}
	}
}

func getCmd(conn net.Conn) []string {
	n, err := conn.Read(srvBuf)
	if err != nil {
		if err != io.EOF {
			util.Log(util.LogErr, "failed getting command. #%s", err)
		}
		return []string{"quit"}
	}
	return cmdSep.Split(strings.TrimSpace(string(srvBuf[:n])), 2)
}
