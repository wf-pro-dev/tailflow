package core

import (
	"log"
	"os"
	"strings"
	"sync"
)

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
)

var (
	logLevel     LogLevel = LogLevelInfo
	logLevelOnce sync.Once
)

func InitLogLevelFromEnv() {
	logLevelOnce.Do(func() {
		switch strings.ToLower(strings.TrimSpace(os.Getenv("TAILFLOW_LOG_LEVEL"))) {
		case "debug":
			logLevel = LogLevelDebug
		case "warn", "warning":
			logLevel = LogLevelWarn
		default:
			logLevel = LogLevelInfo
		}
	})
}

func Debugf(format string, args ...any) {
	InitLogLevelFromEnv()
	if logLevel > LogLevelDebug {
		return
	}
	log.Printf("DEBUG "+format, args...)
}

func Infof(format string, args ...any) {
	InitLogLevelFromEnv()
	if logLevel > LogLevelInfo {
		return
	}
	log.Printf("INFO "+format, args...)
}

func Warnf(format string, args ...any) {
	InitLogLevelFromEnv()
	if logLevel > LogLevelWarn {
		return
	}
	log.Printf("WARN "+format, args...)
}
