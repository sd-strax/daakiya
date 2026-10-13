// Package secrets stores and retrieves secrets in the macOS Keychain through the
// `security` command line tool, so the binary needs no cgo. Secrets are passed to
// `security` on stdin in its interactive mode, never on the command line.
package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const Service = "daakiya"

const (
	AccountLLMKey    = "llm-api-key"
	AccountMSALCache = "msal-token-cache"
)

// EnvLLMKey overrides the Keychain for the LLM key when set.
const EnvLLMKey = "DAAKIYA_LLM_API_KEY"

var ErrNotFound = errors.New("secret not found")

type Store interface {
	Get(account string) (string, error)
	Set(account, value string) error
	Delete(account string) error
}

// Keychain is the macOS implementation.
type Keychain struct{}

func (Keychain) Get(account string) (string, error) {
	if account == AccountLLMKey {
		if v := os.Getenv(EnvLLMKey); v != "" {
			return v, nil
		}
	}
	cmd := exec.Command("security", "find-generic-password", "-a", account, "-s", Service, "-w")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "could not be found") {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain read %s: %v: %s", account, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

func (Keychain) Set(account, value string) error {
	if strings.ContainsAny(value, "\n\"") {
		return errors.New("secret contains characters the keychain helper cannot pass safely")
	}
	// -U updates an existing item in place.
	script := fmt.Sprintf("add-generic-password -a %q -s %q -U -w %q\n", account, Service, value)
	cmd := exec.Command("security", "-i")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain write %s: %v: %s", account, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (Keychain) Delete(account string) error {
	cmd := exec.Command("security", "delete-generic-password", "-a", account, "-s", Service)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "could not be found") {
			return nil
		}
		return fmt.Errorf("keychain delete %s: %v: %s", account, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Memory is an in-memory store for tests.
type Memory struct{ m map[string]string }

func NewMemory() *Memory { return &Memory{m: map[string]string{}} }
func (s *Memory) Get(a string) (string, error) {
	v, ok := s.m[a]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (s *Memory) Set(a, v string) error { s.m[a] = v; return nil }
func (s *Memory) Delete(a string) error { delete(s.m, a); return nil }
