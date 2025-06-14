package utils

import (
	"log"
	"os"
)

// Logger is a wrapper around the standard logger
type Logger struct {
	*log.Logger
}

// NewLogger creates a new logger instance
func NewLogger() *Logger {
	return &Logger{
		Logger: log.New(os.Stdout, "[COUPON-SERVICE] ", log.LstdFlags|log.Lshortfile),
	}
}
