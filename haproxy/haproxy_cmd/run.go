package haproxy_cmd

import (
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/haproxytech/haproxy-consul-connect/haproxy/halog"
	"github.com/haproxytech/haproxy-consul-connect/lib"
	log "github.com/sirupsen/logrus"
)

const (
	// DefaultHAProxyBin is the default HAProxy program name
	DefaultHAProxyBin = "haproxy"

	// haproxyReadyTimeout is the maximum time to wait for HAProxy to be ready
	haproxyReadyTimeout = 30 * time.Second
)

type Config struct {
	HAProxyPath       string
	HAProxyConfigPath string
	MasterRuntime     string
}

func Start(sd *lib.Shutdown, cfg Config) (int, error) {
	// Create a buffered channel to signal when HAProxy is ready
	// Buffered to allow non-blocking sends from multiple log readers
	readyCh := make(chan struct{}, 1)

	// Create a logger that signals when HAProxy is ready
	logger := func(r io.Reader) {
		halog.NewWithReadySignal(r, readyCh)
	}

	haCmd, err := runCommand(sd, logger,
		cfg.HAProxyPath,
		"-W",
		"-S", cfg.MasterRuntime,
		"-f",
		cfg.HAProxyConfigPath,
	)
	if err != nil {
		return 0, err
	}

	if haCmd.Process == nil {
		return 0, fmt.Errorf("HAProxy failed to start")
	}

	// Wait for HAProxy to be ready before returning
	// This prevents sending SIGUSR2 before HAProxy has finished loading
	select {
	case <-readyCh:
		log.Debug("HAProxy is ready to receive configuration updates")
	case <-time.After(haproxyReadyTimeout):
		return 0, fmt.Errorf("timeout waiting for HAProxy to be ready (waited %s)", haproxyReadyTimeout)
	case <-sd.Stop:
		return 0, fmt.Errorf("shutdown requested while waiting for HAProxy to be ready")
	}

	return haCmd.Process.Pid, nil
}

// getVersion Launch Help from program path and Find Version
// to capture the output and retrieve version information
func getVersion(path string) (string, error) {
	cmd := exec.Command(path, "-v")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Failed executing %s: %s", path, err.Error())
	}
	re := regexp.MustCompile("\\d+(\\.\\d+)+")
	return string(re.Find(out)), nil
}

// CheckEnvironment Verifies that all dependencies are correct
func CheckEnvironment(haproxyBin string) error {
	var err error
	wg := &sync.WaitGroup{}
	wg.Add(1)
	ensureVersion := func(path, minVer string, maxVer string) {
		defer wg.Done()
		currVer, e := getVersion(path)
		if e != nil {
			err = e
			return
		}
		res, e := compareVersion(currVer, minVer)
		if e != nil {
			err = e
			return
		}
		if res < 0 {
			err = fmt.Errorf("%s version must be > %s, but is: %s", path, minVer, currVer)
			return
		}
		res, e = compareVersion(currVer, maxVer)
		if e != nil {
			err = e
			return
		}
		if res > 0 {
			err = fmt.Errorf("%s version must be < %s, but is: %s", path, maxVer, currVer)
			return
		}
	}
	go ensureVersion(haproxyBin, "2.0", "4.0")

	wg.Wait()
	if err != nil {
		return err
	}
	return nil
}

// compareVersion compares two semver versions.
// If v1 > v2 returns 1, if v1 < v2 returns -1, if equal returns 0.
// If an error occurs, returns -1 and error.
func compareVersion(v1, v2 string) (int, error) {
	a := strings.Split(v1, ".")
	b := strings.Split(v2, ".")

	if len(a) < 2 {
		return -1, fmt.Errorf("%s arg is not a version string", v1)
	}
	if len(b) < 2 {
		return -1, fmt.Errorf("%s arg is not a version string", v2)
	}

	if len(a) != len(b) {
		switch {
		case len(a) > len(b):
			for i := len(b); len(b) < len(a); i++ {
				b = append(b, " ")
			}
			break
		case len(a) < len(b):
			for i := len(a); len(a) < len(b); i++ {
				a = append(a, " ")
			}
			break
		}
	}

	var res int

	for i, s := range a {
		var ai, bi int
		fmt.Sscanf(s, "%d", &ai)
		fmt.Sscanf(b[i], "%d", &bi)

		if ai > bi {
			res = 1
			break
		}
		if ai < bi {
			res = -1
			break
		}
	}
	return res, nil
}
