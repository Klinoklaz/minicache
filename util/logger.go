package util

import (
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

var logger *log.Logger = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds)

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

	if level >= LogFatal {
		logger.Fatalf(format, a...)
	}

	logger.Printf(format, a...)
}

// not concurrency-safe
func setLogFile(name string) {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		Log(LogWarn, "can't open log file %s, logging destination won't be changed. #%s", name, err)
		return
	}

	logger.SetOutput(f)
}
