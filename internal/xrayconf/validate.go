package xrayconf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Validate runs `xray run -test` on cfg. assetDir holds geoip.dat and
// geosite.dat (XRAY_LOCATION_ASSET). The error carries Xray's own message.
func Validate(ctx context.Context, xrayBin, assetDir string, cfg []byte) error {
	dir, err := os.MkdirTemp("", "sneakernet-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	// Xray picks the config format from the file extension.
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, xrayBin, "run", "-test", "-c", path)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+assetDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xray rejected the config: %s", xrayError(out, err))
	}
	return nil
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// xrayError extracts the useful part of Xray's output: the last line, minus
// the "main: failed to load config files: ... >" prefix chain.
func xrayError(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(ansi.ReplaceAllString(string(out), "")), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if j := strings.LastIndex(l, "> "); j >= 0 {
			l = l[j+2:]
		}
		return l
	}
	return err.Error()
}
