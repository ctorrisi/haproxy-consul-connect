package dataplanelog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"

	log "github.com/sirupsen/logrus"
)

func New(r io.Reader) {
	scan := bufio.NewScanner(r)
	go func() {
		for scan.Scan() {
			logLine(scan.Bytes())
		}
	}()
}

func logLine(line []byte) {
	// dataplane starts by logging stuff in text before switching to json
	if bytes.HasPrefix(line, []byte("time=\"")) {
		return
	}

	f := log.Fields{}
	err := json.Unmarshal(line, &f)
	if err != nil {
		// If we can't parse as JSON, just pass through the raw message
		log.Infof("dataplane: %s", string(line))
		return
	}

	msg, msgOk := f["msg"].(string)
	level, levelOk := f["level"].(string)

	// If the JSON doesn't have the expected fields, pass through raw
	if !msgOk || !levelOk {
		log.Infof("dataplane: %s", string(line))
		return
	}

	delete(f, "msg")
	delete(f, "level")
	delete(f, "time")

	e := log.WithFields(log.Fields(f))
	fn := e.Errorf

	switch strings.ToLower(level) {
	case "panic":
		fn = e.Panicf
	case "fatal":
		fn = e.Fatalf
	case "error":
		fn = e.Errorf
	case "warn", "warning":
		fn = e.Warnf
	case "info":
		fn = e.Infof
	case "debug":
		fn = e.Debugf
	case "trace":
		fn = e.Tracef
	}

	fn("dataplane: %s", msg)
}
