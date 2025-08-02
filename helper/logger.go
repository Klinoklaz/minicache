package helper

import (
	"bufio"
	"log"
	"os"
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
		using bool
	}

	logger *log.Logger = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds)
)

func Log(level int, format string, a ...any) {
	if level < Config.LogLevel {
		return
	}

	// NOTE: use logger.SetPrefix then call logger.Printf is not concurrency-safe
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

	if level >= LogWarn && logFile.using {
		err := logFile.w.Flush()
		if err != nil {
			logFile.using = false
			logger.SetOutput(os.Stderr)
			logger.Printf("error writing log file, fall back to stderr. #%s", err)
			logFile.f.Close()
		}
	}

	if level >= LogFatal {
		os.Exit(1) // log.Fatalf doesn't flush
	}
}

// not concurrency-safe
func setLogFile(name string) {
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		Log(LogWarn, "can't open log file %s, logging destination won't be changed. #%s", name, err)
		return
	}

	logFile.f = f
	logFile.w = bufio.NewWriter(f)
	logger.SetOutput(logFile.w)

	logFile.using = true
}
