package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	MongoDB        MongoDBConfig        `mapstructure:"mongodb"`
	ACME           ACMEConfig           `mapstructure:"acme"`
	Netscaler      NetscalerConfig      `mapstructure:"netscaler"`
	Reconcile      ReconcileConfig      `mapstructure:"reconcile"`
	LeaderElection LeaderElectionConfig `mapstructure:"leader_election"`
}

type LeaderElectionConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	LeaseName     string `mapstructure:"lease_name"`
	Namespace     string `mapstructure:"namespace"`
	LeaseDuration string `mapstructure:"lease_duration"`
	RenewDeadline string `mapstructure:"renew_deadline"`
	RetryInterval string `mapstructure:"retry_interval"`
	Identity      string `mapstructure:"identity"`
}

type ReconcileConfig struct {
	Interval string `mapstructure:"interval"`
}

type MongoDBConfig struct {
	URI        string `mapstructure:"uri"`
	Database   string `mapstructure:"database"`
	Collection string `mapstructure:"collection"`
}

type ACMEConfig struct {
	Email         string `mapstructure:"email"`
	DirectoryURL  string `mapstructure:"directory_url"`
	ChallengePort int    `mapstructure:"challenge_port"`
}

type NetscalerConfig struct {
	Endpoint           string `mapstructure:"endpoint"`
	Username           string `mapstructure:"username"`
	Password           string `mapstructure:"password"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
}

// Load reads the YAML config file then applies environment variable overrides.
// Environment variables use the SDS_ prefix with _ as the key delimiter.
// For example, SDS_MONGODB_URI overrides mongodb.uri.
func Load(path string) (*Config, error) {
	v := viper.New()

	// Defaults.
	v.SetDefault("mongodb.collection", "workspace")
	v.SetDefault("acme.directory_url", "https://acme-v02.api.letsencrypt.org/directory")
	v.SetDefault("acme.challenge_port", 80)
	v.SetDefault("reconcile.interval", "5m")
	v.SetDefault("leader_election.enabled", false)
	v.SetDefault("leader_election.lease_name", "sds-provisioner-leader")
	v.SetDefault("leader_election.namespace", "default")
	v.SetDefault("leader_election.lease_duration", "15s")
	v.SetDefault("leader_election.renew_deadline", "10s")
	v.SetDefault("leader_election.retry_interval", "2s")

	// YAML file.
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Environment variable overrides with SDS_ prefix.
	v.SetEnvPrefix("SDS")
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

// ConfigPath returns the config file path from the SDS_CONFIG_PATH env var,
// or the provided default.
func ConfigPath(defaultPath string) string {
	if v := os.Getenv("SDS_CONFIG_PATH"); v != "" {
		return v
	}
	return defaultPath
}

func validate(c *Config) error {
	if c.MongoDB.URI == "" {
		return fmt.Errorf("mongodb.uri is required (or set SDS_MONGODB_URI)")
	}
	if c.MongoDB.Database == "" {
		return fmt.Errorf("mongodb.database is required (or set SDS_MONGODB_DATABASE)")
	}
	if c.Netscaler.Endpoint == "" {
		return fmt.Errorf("netscaler.endpoint is required (or set SDS_NETSCALER_ENDPOINT)")
	}
	return nil
}
