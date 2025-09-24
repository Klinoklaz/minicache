package util

import (
	"io"
	"log"
	"os"
)

const (
	LogLevelDebug = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelErr
	LogLevelFatal
)

func init() {
	// special mark for messages from go internal packages or other dependencies
	log.SetPrefix("[INTERNAL]")
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lmsgprefix)
}

// use a different logger for application messages
var appLogger = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds)

func doLog(level int, format string, a ...any) {
	if level < Config.LogLevel {
		return
	}

	switch level {
	case LogLevelDebug:
		format = "[DEBUG]" + format
	case LogLevelInfo:
		format = "[INFO]" + format
	case LogLevelWarn:
		format = "[WARNING]" + format
	case LogLevelErr:
		format = "[ERROR]" + format
	case LogLevelFatal:
		format = "[FATAL]" + format
	}

	if level >= LogLevelFatal {
		appLogger.Fatalf(format, a...)
	}

	appLogger.Printf(format, a...)
}

func LogDebug(format string, a ...any) {
	doLog(LogLevelDebug, format, a...)
}

func LogInfo(format string, a ...any) {
	doLog(LogLevelInfo, format, a...)
}

func LogWarn(format string, a ...any) {
	doLog(LogLevelWarn, format, a...)
}

func LogErr(format string, a ...any) {
	doLog(LogLevelErr, format, a...)
}

func LogFatal(format string, a ...any) {
	doLog(LogLevelFatal, format, a...)
}

// not concurrency-safe
func setLogFile(name string) {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err == nil {
		log.SetOutput(f)
		appLogger.SetOutput(f)
		return
	}
	LogWarn("can't open log file %s, logging destination won't be changed. #%s", name, err)
}

func GetLogWriter() io.Writer {
	return appLogger.Writer()
}

func SetLogWriter(w io.Writer) {
	appLogger.SetOutput(w)
}
