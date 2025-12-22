package halog

import (
	"bufio"
	"io"
	"strings"

	log "github.com/sirupsen/logrus"
)

func New(r io.Reader) {
	scan := bufio.NewScanner(r)
	go func() {
		for scan.Scan() {
			haproxyLog(scan.Text())
		}
	}()
}

func haproxyLog(l string) {
	if len(l) == 0 {
		return
	}

	l = strings.TrimSpace(l)
	f := log.Infof

	// Parse log level if present
	if l[0] == '[' {
		end := strings.IndexByte(l, ']')
		if end != -1 {
			switch l[1:end] {
			case "NOTICE":
				f = log.Infof
			case "WARNING":
				f = log.Warnf
			case "ALERT", "ERROR":
				f = log.Errorf
			}
			l = l[end+1:]
		}
	}

	f("haproxy: %s", strings.TrimSpace(l))
}
