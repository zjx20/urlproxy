package logger

import (
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	debug = flag.Bool("debug", false, "Verbose logs")
)

func init() {
	log.SetFlags(log.Lshortfile | log.LstdFlags | log.Lmicroseconds)
}

func SetDebug(v bool) {
	*debug = v
}

func IsDebug() bool {
	return *debug
}

func Debugf(format string, v ...any) {
	if *debug {
		log.Output(2, fmt.Sprintf("[DEBUG] "+format, v...))
	}
}

func Infof(format string, v ...any) {
	log.Output(2, fmt.Sprintf("[INFO] "+format, v...))
}

func Warnf(format string, v ...any) {
	log.Output(2, fmt.Sprintf("[WARN] "+format, v...))
}

func Errorf(format string, v ...any) {
	log.Output(2, fmt.Sprintf("[ERROR] "+format, v...))
}

func Fatalf(format string, v ...any) {
	log.Output(2, fmt.Sprintf("[FATAL] "+format, v...))
	os.Exit(1)
}
