package helper

import (
	"bufio"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const (
	LogDebug = iota
	LogInfo
	LogWarn
	LogErr
	LogFatal
)

var (
	logFile struct {
		w     *bufio.Writer
		f     *os.File // underlying file of the writer
		mtx   sync.Mutex
		inUse bool
	}

	sigs chan os.Signal = make(chan os.Signal, 1)

	logger *log.Logger = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds)
)

func Log(level int, format string, a ...any) {
	if level < Config.LogLevel {
		return
	}

	switch level {
	case LogDebug:
		format = "[DEBUG]" + format
	case LogInfo:
		format = "[INFO]" + format
	case LogWarn:
		format = "[WARNING]" + format
	case LogErr:
		format = "[ERROR]" + format
	case LogFatal:
		format = "[FATAL]" + format
	default:
		format = "[UNKNOWN]" + format
	}

	logger.Printf(format, a...)

	if level >= LogWarn && logFile.inUse {
		logFile.mtx.Lock()
		// kinda tricky to keep this concurrency-safe,
		// is it really worth it to use bufio?
		err := logFile.w.Flush()
		if err != nil && logFile.inUse {
			logger.SetOutput(os.Stderr)
			logFile.inUse = false
			logFile.f.Close()
			logger.Printf("error writing log file, fall back to stderr. #%s", err)
		}
		logFile.mtx.Unlock()
	}

	if level >= LogFatal {
		os.Exit(1) // log.Fatalf doesn't flush
	}
}

// not concurrency-safe
func setLogFile(name string) {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		Log(LogWarn, "can't open log file %s, logging destination won't be changed. #%s", name, err)
		return
	}

	logFile.f = f
	logFile.w = bufio.NewWriter(f)
	logger.SetOutput(logFile.w)

	logFile.inUse = true
}

// handle SIGINT and SIGTERM
func LogSignal() {
	go func() {
		sig := <-sigs
		// trigger a flush
		Log(LogWarn, "received %s signal, terminating process.", sig)
		os.Exit(0)
	}()

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
}
