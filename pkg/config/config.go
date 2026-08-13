// Package config reads and writes kube-ctx's own configuration file, which
// holds context aliases and the guard rules that classify a context as
// production.
//
// The file is optional. A missing or empty config yields built-in defaults, so
// kube-ctx works on a fresh machine with nothing set up.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/somaz94/kube-ctx/pkg/paths"
)

// FileName is the config file's basename inside the kube-ctx config directory.
const FileName = "config.yaml"

// filePerm keeps the config owner-only, consistent with everything else
// kube-ctx writes.
const filePerm = 0o600

// Level is how dangerous a context is considered.
type Level string

const (
	// LevelSafe is the default for anything no guard rule matches.
	LevelSafe Level = "safe"
	// LevelWarn marks a context worth a visible reminder.
	LevelWarn Level = "warn"
	// LevelDanger marks production.
	LevelDanger Level = "danger"
)

// Guard classifies contexts whose name matches a regular expression.
type Guard struct {
	// Match is a regular expression tested against the context name.
	Match string `yaml:"match"`
	// Level is safe, warn, or danger.
	Level Level `yaml:"level"`
	// Confirm requires the user to retype the context name before switching.
	Confirm bool `yaml:"confirm"`
	// Label overrides the badge text shown next to a matching context.
	Label string `yaml:"label,omitempty"`
}

// Config is the whole kube-ctx configuration file.
type Config struct {
	// Aliases maps a short name to a context name.
	Aliases map[string]string `yaml:"aliases,omitempty"`
	// Guards classify contexts. The first matching rule wins.
	Guards []Guard `yaml:"guards,omitempty"`

	// path is where this config was loaded from, so Save round-trips.
	path string
}

// DefaultGuards is used when the config file defines none. It only colors and
// labels — Confirm is off — so a first run never blocks on a prompt the user
// did not ask for. Enabling confirm is a one-line edit documented in the
// generated config.
func DefaultGuards() []Guard {
	return []Guard{
		{Match: `(^|[-_.])(prod|prd|production)([-_.]|$)`, Level: LevelDanger},
		{Match: `(^|[-_.])(stg|stage|staging|uat)([-_.]|$)`, Level: LevelWarn},
	}
}

// Path returns the default config file location.
func Path() (string, error) {
	dir, err := paths.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads the config from its default location.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads the config from path. A missing file is not an error: it
// yields a config with the default guards and no aliases.
func LoadFrom(path string) (*Config, error) {
	cfg := &Config{path: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg.Guards = DefaultGuards()
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if len(cfg.Guards) == 0 {
		cfg.Guards = DefaultGuards()
	}
	return cfg, nil
}

// Path returns where this config lives on disk.
func (c *Config) Path() string { return c.path }

// Save writes the config back, creating the directory if needed.
func (c *Config) Save() error {
	if c.path == "" {
		path, err := Path()
		if err != nil {
			return err
		}
		c.path = path
	}
	if err := paths.EnsureDir(filepath.Dir(c.path)); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(c.path, data, filePerm); err != nil {
		return fmt.Errorf("write config %s: %w", c.path, err)
	}
	return nil
}

// ResolveAlias maps an alias to its context name, returning name unchanged when
// it is not an alias. A leading "@" is accepted so users can force the alias
// reading of a name that also exists as a context.
func (c *Config) ResolveAlias(name string) string {
	explicit := strings.HasPrefix(name, "@")
	key := strings.TrimPrefix(name, "@")

	if target, ok := c.Aliases[key]; ok {
		return target
	}
	if explicit {
		return key
	}
	return name
}

// SetAlias records an alias.
func (c *Config) SetAlias(alias, target string) error {
	alias = strings.TrimPrefix(alias, "@")
	if alias == "" {
		return fmt.Errorf("the alias must not be empty")
	}
	if target == "" {
		return fmt.Errorf("the target context must not be empty")
	}
	if c.Aliases == nil {
		c.Aliases = map[string]string{}
	}
	c.Aliases[alias] = target
	return nil
}

// DeleteAlias removes an alias.
func (c *Config) DeleteAlias(alias string) error {
	alias = strings.TrimPrefix(alias, "@")
	if _, ok := c.Aliases[alias]; !ok {
		return fmt.Errorf("no alias named %q", alias)
	}
	delete(c.Aliases, alias)
	return nil
}

// AliasPair is one alias-to-context mapping.
type AliasPair struct {
	Alias  string
	Target string
}

// AliasList returns every alias sorted by name.
func (c *Config) AliasList() []AliasPair {
	out := make([]AliasPair, 0, len(c.Aliases))
	for alias, target := range c.Aliases {
		out = append(out, AliasPair{Alias: alias, Target: target})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}
