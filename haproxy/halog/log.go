package halog

import (
	"bufio"
	"io"
	"strings"

	log "github.com/sirupsen/logrus"
)

// New creates a log reader that reads HAProxy output and logs it.
// It returns immediately and processes logs asynchronously.
func New(r io.Reader) {
	NewWithReadySignal(r, nil)
}

// NewWithReadySignal creates a log reader that reads HAProxy output and logs it.
// If readyCh is provided (must be buffered with capacity >= 1), a signal will be
// sent when HAProxy signals it's ready (by printing "Loading success.").
// This function can be called multiple times with the same channel (e.g., for
// stdout and stderr) - only the first signal will be sent.
func NewWithReadySignal(r io.Reader, readyCh chan struct{}) {
	scan := bufio.NewScanner(r)
	go func() {
		for scan.Scan() {
			line := scan.Text()
			haproxyLog(line)

			// Detect HAProxy readiness - it prints "Loading success." when the
			// first worker has finished loading and it's ready to receive reloads
			if readyCh != nil && strings.Contains(line, "Loading success") {
				// Non-blocking send - only the first one will succeed
				select {
				case readyCh <- struct{}{}:
				default:
				}
			}
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
