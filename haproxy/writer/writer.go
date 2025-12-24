package writer

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	log "github.com/sirupsen/logrus"
)

type ConfigWriter struct {
	configPath string
	haproxyBin string
	masterPID  int
}

func New(configPath, haproxyBin string, masterPID int) *ConfigWriter {
	return &ConfigWriter{
		configPath: configPath,
		haproxyBin: haproxyBin,
		masterPID:  masterPID,
	}
}

func (w *ConfigWriter) ApplyConfig(config string) error {
	tmpPath := w.configPath + ".new"

	// Write to temp file
	err := os.WriteFile(tmpPath, []byte(config), 0600)
	if err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}

	// Validate config
	cmd := exec.Command(w.haproxyBin, "-c", "-f", tmpPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Remove invalid temp file
		os.Remove(tmpPath)
		return fmt.Errorf("config validation failed: %w\nOutput: %s", err, string(output))
	}

	// Atomic rename
	err = os.Rename(tmpPath, w.configPath)
	if err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	// Send SIGUSR2 to master process for graceful reload
	err = syscall.Kill(w.masterPID, syscall.SIGUSR2)
	if err != nil {
		return fmt.Errorf("failed to send SIGUSR2 to HAProxy master (pid %d): %w", w.masterPID, err)
	}

	log.Info("HAProxy configuration reloaded successfully")
	return nil
}
