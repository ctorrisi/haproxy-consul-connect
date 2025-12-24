package haproxy_cmd

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/haproxytech/haproxy-consul-connect/haproxy/halog"
	"github.com/haproxytech/haproxy-consul-connect/lib"
)

const (
	// DefaultHAProxyBin is the default HAProxy program name
	DefaultHAProxyBin = "haproxy"
)

type Config struct {
	HAProxyPath       string
	HAProxyConfigPath string
	MasterRuntime     string
}

func Start(sd *lib.Shutdown, cfg Config) (int, error) {
	haCmd, err := runCommand(sd, halog.New,
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
