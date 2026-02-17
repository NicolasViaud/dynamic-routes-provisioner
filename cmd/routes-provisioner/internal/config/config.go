package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Log              LogConfig              `mapstructure:"log"`
	Datasource       DatasourceConfig       `mapstructure:"datasource"`
	Certificate      CertificateConfig      `mapstructure:"certificate"`
	CertificateStore CertificateStoreConfig `mapstructure:"certificate_store"`
	Provisioner      ProvisionerConfig      `mapstructure:"provisioner"`
	Reconcile        ReconcileConfig        `mapstructure:"reconcile"`
	LeaderElection   LeaderElectionConfig   `mapstructure:"leader_election"`
}

type LogConfig struct {
	Format string `mapstructure:"format"` // "json" or "text"
}

type LeaderElectionConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Provider      string `mapstructure:"provider"` // "kube" or "mongo"
	LeaseName     string `mapstructure:"lease_name"`
	Namespace     string `mapstructure:"namespace"` // kube only
	LeaseDuration string `mapstructure:"lease_duration"`
	RenewDeadline string `mapstructure:"renew_deadline"` // kube only
	RetryInterval string `mapstructure:"retry_interval"`
	Identity      string `mapstructure:"identity"`
}

type CertificateStoreConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	Provider     string `mapstructure:"provider"`      // "kube", "vault", or "file"
	Namespace    string `mapstructure:"namespace"`     // kube only
	SecretPrefix string `mapstructure:"secret_prefix"` // kube only
	RenewBefore  string `mapstructure:"renew_before"`
	Dir          string `mapstructure:"dir"`           // file only — directory to store certs (default ".certs")
	VaultAddress string `mapstructure:"vault_address"` // vault only
	VaultToken   string `mapstructure:"vault_token"`   // vault only
	VaultMount   string `mapstructure:"vault_mount"`   // vault only
	VaultPrefix  string `mapstructure:"vault_prefix"`  // vault only
}

type ReconcileConfig struct {
	Interval string `mapstructure:"interval"`
}

type DatasourceConfig struct {
	Provider   string       `mapstructure:"provider"` // "mongodb" or "http"
	URI        string       `mapstructure:"uri"`
	Database   string       `mapstructure:"database"`
	Collection string       `mapstructure:"collection"`
	ListenAddr string       `mapstructure:"listen_addr"` // http only — address for the HTTP source API (default ":8081")
	Mapper     MapperConfig `mapstructure:"mapper"`      // mongodb only — document-to-route mapping
}

type MapperConfig struct {
	URLField string          `mapstructure:"url_field"` // document field containing the URL (default "url")
	Path     string          `mapstructure:"path"`      // default route path (default "/")
	TLS      bool            `mapstructure:"tls"`       // whether to enable TLS (default true)
	Backends []BackendConfig `mapstructure:"backends"`  // fixed backends applied to every route
}

type BackendConfig struct {
	ServiceName string `mapstructure:"service_name"`
	Port        int    `mapstructure:"port"`
	Weight      int    `mapstructure:"weight"`
}

type CertificateConfig struct {
	Provider      string `mapstructure:"provider"`       // "acme-http", "acme-dns", "selfsigned", or "vault"
	Email         string `mapstructure:"email"`          // acme-http/acme-dns
	DirectoryURL  string `mapstructure:"directory_url"`  // acme-http/acme-dns
	ChallengePort int    `mapstructure:"challenge_port"` // acme-http only
	Validity      string `mapstructure:"validity"`       // selfsigned only
	Organization  string `mapstructure:"organization"`   // selfsigned only
	VaultAddress  string `mapstructure:"vault_address"`  // vault only
	VaultToken    string `mapstructure:"vault_token"`    // vault only
	VaultMount    string `mapstructure:"vault_mount"`    // vault only
	VaultRole     string `mapstructure:"vault_role"`     // vault only
	VaultTTL      string `mapstructure:"vault_ttl"`      // vault only
}

type ProvisionerConfig struct {
	Provider           string `mapstructure:"provider"` // "netscaler", "ingress", or "log"
	Endpoint           string `mapstructure:"endpoint"`
	Username           string `mapstructure:"username"`
	Password           string `mapstructure:"password"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
	// --- ingress only ---
	Namespace           string `mapstructure:"namespace"`              // K8s namespace for Ingress resources
	MaxRoutesPerIngress int    `mapstructure:"max_routes_per_ingress"` // max rules per Ingress bucket
	IngressClass        string `mapstructure:"ingress_class"`          // IngressClassName (empty for default)
}

// Load reads the YAML config file then applies environment variable overrides.
// Environment variables use the ROUTES_ prefix with _ as the key delimiter.
// For example, ROUTES_MONGODB_URI overrides mongodb.uri.
func Load(path string) (*Config, error) {
	v := viper.New()

	// Defaults.
	v.SetDefault("log.format", "json")
	v.SetDefault("datasource.provider", "mongodb")
	v.SetDefault("datasource.collection", "routes")
	v.SetDefault("datasource.listen_addr", ":8081")
	v.SetDefault("datasource.mapper.url_field", "url")
	v.SetDefault("datasource.mapper.path", "/")
	v.SetDefault("datasource.mapper.tls", true)
	v.SetDefault("certificate.provider", "acme-http")
	v.SetDefault("certificate.directory_url", "https://acme-v02.api.letsencrypt.org/directory")
	v.SetDefault("certificate.challenge_port", 80)
	v.SetDefault("certificate.validity", "8760h")
	v.SetDefault("certificate.organization", "Self-Signed")
	v.SetDefault("certificate.vault_mount", "pki")
	v.SetDefault("certificate.vault_ttl", "720h")
	v.SetDefault("certificate_store.enabled", false)
	v.SetDefault("certificate_store.provider", "kube")
	v.SetDefault("certificate_store.namespace", "default")
	v.SetDefault("certificate_store.secret_prefix", "route-tls")
	v.SetDefault("certificate_store.renew_before", "720h")
	v.SetDefault("certificate_store.dir", ".certs")
	v.SetDefault("certificate_store.vault_mount", "secret")
	v.SetDefault("certificate_store.vault_prefix", "route-tls")
	v.SetDefault("provisioner.provider", "netscaler")
	v.SetDefault("provisioner.namespace", "default")
	v.SetDefault("provisioner.max_routes_per_ingress", 50)
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
	switch c.Datasource.Provider {
	case "mongodb":
		if c.Datasource.URI == "" {
			return fmt.Errorf("datasource.uri is required (or set ROUTES_DATASOURCE_URI)")
		}
		if c.Datasource.Database == "" {
			return fmt.Errorf("datasource.database is required (or set ROUTES_DATASOURCE_DATABASE)")
		}
	case "http":
		// No extra validation needed — listen_addr has a default.
	default:
		return fmt.Errorf("datasource.provider must be 'mongodb' or 'http' (or set ROUTES_DATASOURCE_PROVIDER)")
	}
	switch c.Provisioner.Provider {
	case "netscaler":
		if c.Provisioner.Endpoint == "" {
			return fmt.Errorf("provisioner.endpoint is required (or set ROUTES_PROVISIONER_ENDPOINT)")
		}
	case "ingress":
		// Namespace has a default; no extra validation needed.
	case "log":
		// No validation needed.
	default:
		return fmt.Errorf("provisioner.provider must be 'netscaler', 'ingress', or 'log' (or set ROUTES_PROVISIONER_PROVIDER)")
	}
	switch c.Certificate.Provider {
	case "acme-http", "acme-dns", "selfsigned", "vault":
	default:
		return fmt.Errorf("certificate.provider must be 'acme-http', 'acme-dns', 'selfsigned', or 'vault' (or set ROUTES_CERTIFICATE_PROVIDER)")
	}
	if c.CertificateStore.Enabled {
		switch c.CertificateStore.Provider {
		case "kube", "vault", "file":
		default:
			return fmt.Errorf("certificate_store.provider must be 'kube', 'vault', or 'file' (or set ROUTES_CERTIFICATE_STORE_PROVIDER)")
		}
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
