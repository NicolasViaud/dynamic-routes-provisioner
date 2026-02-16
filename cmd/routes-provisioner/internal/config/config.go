package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Datasource     DatasourceConfig     `mapstructure:"datasource"`
	Certificate    CertificateConfig    `mapstructure:"certificate"`
	Provisioner    ProvisionerConfig    `mapstructure:"provisioner"`
	Reconcile      ReconcileConfig      `mapstructure:"reconcile"`
	LeaderElection LeaderElectionConfig `mapstructure:"leader_election"`
}

type LeaderElectionConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Provider      string `mapstructure:"provider"` // "kube" or "mongo"
	LeaseName     string `mapstructure:"lease_name"`
	Namespace     string `mapstructure:"namespace"`     // kube only
	LeaseDuration string `mapstructure:"lease_duration"`
	RenewDeadline string `mapstructure:"renew_deadline"` // kube only
	RetryInterval string `mapstructure:"retry_interval"`
	Identity      string `mapstructure:"identity"`
}

type ReconcileConfig struct {
	Interval string `mapstructure:"interval"`
}

type DatasourceConfig struct {
	Provider   string `mapstructure:"provider"` // "mongodb"
	URI        string `mapstructure:"uri"`
	Database   string `mapstructure:"database"`
	Collection string `mapstructure:"collection"`
}

type CertificateConfig struct {
	Provider      string `mapstructure:"provider"` // "acme" or "selfsigned"
	Email         string `mapstructure:"email"`
	DirectoryURL  string `mapstructure:"directory_url"`
	ChallengePort int    `mapstructure:"challenge_port"`
	Validity      string `mapstructure:"validity"`     // selfsigned only
	Organization  string `mapstructure:"organization"` // selfsigned only
}

type ProvisionerConfig struct {
	Provider           string `mapstructure:"provider"` // "netscaler"
	Endpoint           string `mapstructure:"endpoint"`
	Username           string `mapstructure:"username"`
	Password           string `mapstructure:"password"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
}

// Load reads the YAML config file then applies environment variable overrides.
// Environment variables use the ROUTES_ prefix with _ as the key delimiter.
// For example, ROUTES_MONGODB_URI overrides mongodb.uri.
func Load(path string) (*Config, error) {
	v := viper.New()

	// Defaults.
	v.SetDefault("datasource.provider", "mongodb")
	v.SetDefault("datasource.collection", "workspace")
	v.SetDefault("certificate.provider", "acme")
	v.SetDefault("certificate.directory_url", "https://acme-v02.api.letsencrypt.org/directory")
	v.SetDefault("certificate.challenge_port", 80)
	v.SetDefault("certificate.validity", "8760h")
	v.SetDefault("certificate.organization", "Self-Signed")
	v.SetDefault("provisioner.provider", "netscaler")
	v.SetDefault("reconcile.interval", "5m")
	v.SetDefault("leader_election.enabled", false)
	v.SetDefault("leader_election.provider", "kube")
	v.SetDefault("leader_election.lease_name", "routes-provisioner-leader")
	v.SetDefault("leader_election.namespace", "default")
	v.SetDefault("leader_election.lease_duration", "15s")
	v.SetDefault("leader_election.renew_deadline", "10s")
	v.SetDefault("leader_election.retry_interval", "2s")

	// YAML file.
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Environment variable overrides with ROUTES_ prefix.
	v.SetEnvPrefix("ROUTES")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// ConfigPath returns the config file path from the ROUTES_CONFIG_PATH env var,
// or the provided default.
func ConfigPath(defaultPath string) string {
	if v := os.Getenv("ROUTES_CONFIG_PATH"); v != "" {
		return v
	}
	return defaultPath
}

func validate(c *Config) error {
	if c.Datasource.URI == "" {
		return fmt.Errorf("datasource.uri is required (or set ROUTES_DATASOURCE_URI)")
	}
	if c.Datasource.Database == "" {
		return fmt.Errorf("datasource.database is required (or set ROUTES_DATASOURCE_DATABASE)")
	}
	if c.Provisioner.Endpoint == "" {
		return fmt.Errorf("provisioner.endpoint is required (or set ROUTES_PROVISIONER_ENDPOINT)")
	}
	switch c.Certificate.Provider {
	case "acme", "selfsigned":
	default:
		return fmt.Errorf("certificate.provider must be 'acme' or 'selfsigned' (or set ROUTES_CERTIFICATE_PROVIDER)")
	}
	if c.LeaderElection.Enabled {
		switch c.LeaderElection.Provider {
		case "kube", "mongo":
		default:
			return fmt.Errorf("leader_election.provider must be 'kube' or 'mongo' (or set ROUTES_LEADER_ELECTION_PROVIDER)")
		}
	}
	return nil
}
