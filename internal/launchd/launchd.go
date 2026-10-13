// Package launchd installs and removes the LaunchAgent that runs `daakiya run`.
package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

const Label = "dev.daakiya.run"

func plist(binary string, interval int, logFile string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
  </array>
  <key>StartInterval</key><integer>%d</integer>
  <key>RunAtLoad</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin</string>
  </dict>
</dict>
</plist>
`, Label, binary, interval, logFile, logFile)
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// Install writes the plist and loads it.
func Install(plistPath string, interval int, logFile string) error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if binary, err = filepath.EvalSymlinks(binary); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", domain(), plistPath).Run()
	if err := os.WriteFile(plistPath, []byte(plist(binary, interval, logFile)), 0o644); err != nil {
		return err
	}
	out, err := exec.Command("launchctl", "bootstrap", domain(), plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	return nil
}

// Uninstall unloads and removes the plist.
func Uninstall(plistPath string) error {
	_ = exec.Command("launchctl", "bootout", domain(), plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Installed reports whether the plist exists.
func Installed(plistPath string) bool {
	_, err := os.Stat(plistPath)
	return err == nil
}
