package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds persisted user preferences for klens.
type Config struct {
	// ReadOnly prevents all cluster-mutating operations when true. The
	// --readonly CLI flag overrides this (forces true) but never forces false.
	ReadOnly bool `json:"read_only,omitempty"`
	// Namespaces maps cluster context name → manually saved namespace list.
	Namespaces map[string][]string `json:"namespaces"`
	// LastNamespace maps cluster context name → last active namespace.
	LastNamespace map[string]string `json:"last_namespace"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "klens", "config.json")
}

// Load reads config from disk. Returns an empty config (not an error) if the
// file doesn't exist yet.
func Load() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		return &Config{
			Namespaces:    make(map[string][]string),
			LastNamespace: make(map[string]string),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
		return &Config{Namespaces: make(map[string][]string)}, nil
	}
	if cfg.Namespaces == nil {
		cfg.Namespaces = make(map[string][]string)
	}
	if cfg.LastNamespace == nil {
		cfg.LastNamespace = make(map[string]string)
	}
	return &cfg, nil
}

// Save writes the config to disk, creating parent directories as needed.
func (c *Config) Save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// SavedNamespaces returns the manually saved namespaces for a cluster context.
func (c *Config) SavedNamespaces(clusterContext string) []string {
	return c.Namespaces[clusterContext]
}

// AddNamespace adds ns to the saved list for clusterContext (no-op if already present).
func (c *Config) AddNamespace(clusterContext, ns string) {
	for _, n := range c.Namespaces[clusterContext] {
		if n == ns {
			return
		}
	}
	c.Namespaces[clusterContext] = append(c.Namespaces[clusterContext], ns)
}

// RemoveNamespace removes ns from the saved list for clusterContext.
func (c *Config) RemoveNamespace(clusterContext, ns string) {
	existing := c.Namespaces[clusterContext]
	out := existing[:0]
	for _, n := range existing {
		if n != ns {
			out = append(out, n)
		}
	}
	c.Namespaces[clusterContext] = out
}

// GetLastNamespace returns the last active namespace for clusterContext, or ""
// if none was saved.
func (c *Config) GetLastNamespace(clusterContext string) string {
	return c.LastNamespace[clusterContext]
}

// SetLastNamespace records ns as the last active namespace for clusterContext.
func (c *Config) SetLastNamespace(clusterContext, ns string) {
	c.LastNamespace[clusterContext] = ns
}
