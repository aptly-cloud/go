package aptly

import (
	"fmt"
	"log"
	"os"
)

// Logger provides logging functionality for the SDK
type Logger struct {
	verbose bool
	logger  *log.Logger
}

// NewLogger creates a new logger instance
func NewLogger(verbose bool) *Logger {
	return &Logger{
		verbose: verbose,
		logger:  log.New(os.Stdout, "[Aptly] ", log.LstdFlags),
	}
}

// Info logs informational messages
func (l *Logger) Info(v ...interface{}) {
	if l.verbose {
		l.logger.Println(append([]interface{}{"[INFO]"}, v...)...)
	}
}

// Infof logs formatted informational messages
func (l *Logger) Infof(format string, v ...interface{}) {
	if l.verbose {
		l.logger.Printf("[INFO] "+format, v...)
	}
}

// Error logs error messages
func (l *Logger) Error(v ...interface{}) {
	l.logger.Println(append([]interface{}{"[ERROR]"}, v...)...)
}

// Errorf logs formatted error messages
func (l *Logger) Errorf(format string, v ...interface{}) {
	l.logger.Printf("[ERROR] "+format, v...)
}

// Warn logs warning messages
func (l *Logger) Warn(v ...interface{}) {
	if l.verbose {
		l.logger.Println(append([]interface{}{"[WARN]"}, v...)...)
	}
}

// Warnf logs formatted warning messages
func (l *Logger) Warnf(format string, v ...interface{}) {
	if l.verbose {
		l.logger.Printf("[WARN] "+format, v...)
	}
}

// Debug logs debug messages
func (l *Logger) Debug(v ...interface{}) {
	if l.verbose {
		l.logger.Println(append([]interface{}{"[DEBUG]"}, v...)...)
	}
}

// Debugf logs formatted debug messages
func (l *Logger) Debugf(format string, v ...interface{}) {
	if l.verbose {
		l.logger.Printf("[DEBUG] "+format, v...)
	}
}

// Print is a simple print wrapper
func (l *Logger) Print(v ...interface{}) {
	fmt.Println(v...)
}
