package logger

import (
	"flag"
	"log"
)

var (
	debug = flag.Bool("debug", false, "Verbose logs")
)

func init() {
	log.SetFlags(log.Lshortfile | log.LstdFlags | log.Lmicroseconds)
}

func IsDebug() bool {
	return *debug
}

func Debugf(format string, v ...any) {
	if *debug {
		log.Printf("[DEBUG] "+format, v...)
	}
}

func Infof(format string, v ...any) {
	log.Printf("[INFO] "+format, v...)
}

func Warnf(format string, v ...any) {
	log.Printf("[WARN] "+format, v...)
}

func Errorf(format string, v ...any) {
	log.Printf("[ERROR] "+format, v...)
}

func Fatalf(format string, v ...any) {
	log.Fatalf("[FATAL] "+format, v...)
}
