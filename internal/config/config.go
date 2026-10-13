// Package config loads and saves Daakiya's non-secret configuration (TOML) and
// knows where everything lives on disk.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	ModeShadow = "shadow"
	ModeLive   = "live"

	ProviderAnthropic    = "anthropic"
	ProviderOpenAICompat = "openai-compatible"
)

type Config struct {
	Mode            string `toml:"mode"`
	IntervalSeconds int    `toml:"interval_seconds"`

	Mailbox Mailbox `toml:"mailbox"`
	LLM     LLM     `toml:"llm"`
	Owner   Owner   `toml:"owner"`
	Privacy Privacy `toml:"privacy"`
	Shadow  Shadow  `toml:"shadow"`
	Log     Log     `toml:"log"`
}

type Mailbox struct {
	Provider       string `toml:"provider"`
	ClientID       string `toml:"client_id"`
	TenantID       string `toml:"tenant_id"`
	FolderName     string `toml:"folder_name"`
	SubfolderName  string `toml:"subfolder_name"`
	Category       string `toml:"category"`
	ShadowCategory string `toml:"shadow_category"`
	// CorrespondentMonths is how far back Sent Items is scanned for known people.
	CorrespondentMonths int `toml:"correspondent_months"`
}

type LLM struct {
	Provider       string  `toml:"provider"`
	Model          string  `toml:"model"`
	BaseURL        string  `toml:"base_url"`
	Threshold      float64 `toml:"threshold"`
	MaxBodyChars   int     `toml:"max_body_chars"`
	MaxCallsPerDay int     `toml:"max_calls_per_day"`
	Concurrency    int     `toml:"concurrency"`
	JSONMode       bool    `toml:"json_mode"`
	MaxExamples    int     `toml:"max_examples"`
	// Prices are USD per million tokens, used only for the local cost estimate.
	InputPricePerM  float64 `toml:"input_price_per_m"`
	OutputPricePerM float64 `toml:"output_price_per_m"`
}

type Owner struct {
	Profile string   `toml:"profile"`
	Domains []string `toml:"domains"`
}

type Privacy struct {
	Redact bool `toml:"redact"`
}

type Shadow struct {
	Tag bool `toml:"tag"`
}

type Log struct {
	Verbose bool `toml:"verbose"`
}

// Default returns a config with every default filled in.
func Default() Config {
	return Config{
		Mode:            ModeShadow,
		IntervalSeconds: 300,
		Mailbox: Mailbox{
			Provider:            "graph",
			TenantID:            "common",
			FolderName:          "Daakiya",
			SubfolderName:       "Cold Outreach",
			Category:            "Daakiya",
			ShadowCategory:      "Daakiya: would move",
			CorrespondentMonths: 24,
		},
		LLM: LLM{
			Provider:        ProviderAnthropic,
			Model:           "claude-haiku-5-5",
			Threshold:       0.85,
			MaxBodyChars:    1500,
			MaxCallsPerDay:  500,
			Concurrency:     4,
			JSONMode:        true,
			MaxExamples:     12,
			InputPricePerM:  0.10,
			OutputPricePerM: 0.50,
		},
		Shadow: Shadow{Tag: true},
	}
}

// Paths groups the on-disk locations. All are under the user's home.
type Paths struct {
	ConfigFile   string
	DataDir      string
	DBFile       string
	LogFile      string
	LockFile     string
	LaunchdPlist string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	data := filepath.Join(home, "Library", "Application Support", "daakiya")
	return Paths{
		ConfigFile:   filepath.Join(home, ".config", "daakiya", "config.toml"),
		DataDir:      data,
		DBFile:       filepath.Join(data, "decisions.sqlite"),
		LogFile:      filepath.Join(data, "daakiya.log"),
		LockFile:     filepath.Join(data, "run.lock"),
		LaunchdPlist: filepath.Join(home, "Library", "LaunchAgents", "dev.daakiya.run.plist"),
	}, nil
}

var ErrNotInitialized = errors.New("daakiya is not initialised; run `daakiya init`")

// Load reads the config file, layering it over defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, ErrNotInitialized
		}
		return cfg, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, cfg.Validate()
}

// Save writes the config with 0600 permissions, creating the directory.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

func (c Config) Validate() error {
	switch c.Mode {
	case ModeShadow, ModeLive:
	default:
		return fmt.Errorf("mode must be %q or %q, got %q", ModeShadow, ModeLive, c.Mode)
	}
	switch c.LLM.Provider {
	case ProviderAnthropic:
	case ProviderOpenAICompat:
		if c.LLM.BaseURL == "" {
			return errors.New("llm.base_url is required for the openai-compatible provider")
		}
	default:
		return fmt.Errorf("llm.provider must be %q or %q", ProviderAnthropic, ProviderOpenAICompat)
	}
	if c.Mailbox.ClientID == "" {
		return errors.New("mailbox.client_id is required")
	}
	if c.LLM.Threshold <= 0 || c.LLM.Threshold > 1 {
		return errors.New("llm.threshold must be in (0, 1]")
	}
	return nil
}
