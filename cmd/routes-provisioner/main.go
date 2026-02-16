package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nicol/dynamic-route-provisioner/core/orchestrator"
	"github.com/nicol/dynamic-route-provisioner/core/reconciler"

	corecert "github.com/nicol/dynamic-route-provisioner/core/certificate"

	certacmedns "github.com/nicol/dynamic-route-provisioner/cert-acme-dns"
	certacmehttp "github.com/nicol/dynamic-route-provisioner/cert-acme-http"
	certselfsigned "github.com/nicol/dynamic-route-provisioner/cert-selfsigned"
	certvault "github.com/nicol/dynamic-route-provisioner/cert-vault"
	certstorekube "github.com/nicol/dynamic-route-provisioner/certstore-kube"
	certstorevault "github.com/nicol/dynamic-route-provisioner/certstore-vault"
	corelease "github.com/nicol/dynamic-route-provisioner/core/lease"
	leasekube "github.com/nicol/dynamic-route-provisioner/lease-kube"
	leasemongo "github.com/nicol/dynamic-route-provisioner/lease-mongo"
	provnetscaler "github.com/nicol/dynamic-route-provisioner/provisioner-netscaler"
	sourcemongo "github.com/nicol/dynamic-route-provisioner/source-mongo"

	"github.com/nicol/dynamic-route-provisioner/routes-provisioner/internal/certificate"
	"github.com/nicol/dynamic-route-provisioner/routes-provisioner/internal/config"
	"github.com/nicol/dynamic-route-provisioner/routes-provisioner/internal/provisioner"
	"github.com/nicol/dynamic-route-provisioner/routes-provisioner/internal/source"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	configFlag := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	configPath := config.ConfigPath(*configFlag)

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "path", configPath, "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- MongoDB trigger ---
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.Datasource.URI))
	if err != nil {
		logger.Error("failed to connect to mongodb", "error", err)
		os.Exit(1)
	}
	defer mongoClient.Disconnect(ctx)

	collection := mongoClient.Database(cfg.Datasource.Database).Collection(cfg.Datasource.Collection)
	trig := sourcemongo.New(collection, &source.MongoMapper{})

	// --- Certificate issuer ---
	var issuer corecert.Issuer
	switch cfg.Certificate.Provider {
	case "acme-http":
		challengeServer := certificate.NewChallengeServer(cfg.Certificate.ChallengePort)
		acmeOpts := []certacmehttp.Option{
			certacmehttp.WithEmail(cfg.Certificate.Email),
		}
		if cfg.Certificate.DirectoryURL != "" {
			acmeOpts = append(acmeOpts, certacmehttp.WithDirectoryURL(cfg.Certificate.DirectoryURL))
		}
		var err error
		issuer, err = certacmehttp.New(challengeServer, acmeOpts...)
		if err != nil {
			logger.Error("failed to create acme issuer", "error", err)
			os.Exit(1)
		}
	case "acme-dns":
		// TODO: wire a concrete DNSProvider implementation here.
		// Example: dnsProvider := mydns.NewCloudflareProvider(...)
		var dnsProvider certacmedns.DNSProvider
		dnsOpts := []certacmedns.Option{
			certacmedns.WithEmail(cfg.Certificate.Email),
		}
		if cfg.Certificate.DirectoryURL != "" {
			dnsOpts = append(dnsOpts, certacmedns.WithDirectoryURL(cfg.Certificate.DirectoryURL))
		}
		var err error
		issuer, err = certacmedns.New(dnsProvider, dnsOpts...)
		if err != nil {
			logger.Error("failed to create acme-dns issuer", "error", err)
			os.Exit(1)
		}
	case "selfsigned":
		var ssOpts []certselfsigned.Option
		if cfg.Certificate.Validity != "" {
			validity, err := time.ParseDuration(cfg.Certificate.Validity)
			if err != nil {
				logger.Error("invalid certificate.validity", "value", cfg.Certificate.Validity, "error", err)
				os.Exit(1)
			}
			ssOpts = append(ssOpts, certselfsigned.WithValidity(validity))
		}
		if cfg.Certificate.Organization != "" {
			ssOpts = append(ssOpts, certselfsigned.WithOrganization(cfg.Certificate.Organization))
		}
		issuer = certselfsigned.New(ssOpts...)
	case "vault":
		vaultOpts := []certvault.Option{
			certvault.WithRole(cfg.Certificate.VaultRole),
		}
		if cfg.Certificate.VaultAddress != "" {
			vaultOpts = append(vaultOpts, certvault.WithAddress(cfg.Certificate.VaultAddress))
		}
		if cfg.Certificate.VaultToken != "" {
			vaultOpts = append(vaultOpts, certvault.WithToken(cfg.Certificate.VaultToken))
		}
		if cfg.Certificate.VaultMount != "" {
			vaultOpts = append(vaultOpts, certvault.WithMount(cfg.Certificate.VaultMount))
		}
		if cfg.Certificate.VaultTTL != "" {
			vaultOpts = append(vaultOpts, certvault.WithTTL(cfg.Certificate.VaultTTL))
		}
		var err error
		issuer, err = certvault.New(vaultOpts...)
		if err != nil {
			logger.Error("failed to create vault issuer", "error", err)
			os.Exit(1)
		}
	}
	logger.Info("certificate issuer configured", "provider", cfg.Certificate.Provider)

	// --- Kubernetes clientset (shared by certificate store and kube leader election) ---
	needsKube := (cfg.CertificateStore.Enabled && cfg.CertificateStore.Provider == "kube") || (cfg.LeaderElection.Enabled && cfg.LeaderElection.Provider == "kube")
	var clientset kubernetes.Interface
	if needsKube {
		kubeConfig, err := rest.InClusterConfig()
		if err != nil {
			logger.Error("failed to get in-cluster config", "error", err)
			os.Exit(1)
		}
		clientset, err = kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			logger.Error("failed to create kubernetes client", "error", err)
			os.Exit(1)
		}
	}

	// --- Certificate store (caching decorator) ---
	if cfg.CertificateStore.Enabled {
		renewBefore, err := time.ParseDuration(cfg.CertificateStore.RenewBefore)
		if err != nil {
			logger.Error("invalid certificate_store.renew_before", "value", cfg.CertificateStore.RenewBefore, "error", err)
			os.Exit(1)
		}

		switch cfg.CertificateStore.Provider {
		case "kube":
			issuer = certstorekube.New(issuer, clientset, logger,
				certstorekube.WithNamespace(cfg.CertificateStore.Namespace),
				certstorekube.WithSecretPrefix(cfg.CertificateStore.SecretPrefix),
				certstorekube.WithRenewBefore(renewBefore),
			)
		case "vault":
			storeOpts := []certstorevault.Option{
				certstorevault.WithRenewBefore(renewBefore),
			}
			if cfg.CertificateStore.VaultAddress != "" {
				storeOpts = append(storeOpts, certstorevault.WithAddress(cfg.CertificateStore.VaultAddress))
			}
			if cfg.CertificateStore.VaultToken != "" {
				storeOpts = append(storeOpts, certstorevault.WithToken(cfg.CertificateStore.VaultToken))
			}
			if cfg.CertificateStore.VaultMount != "" {
				storeOpts = append(storeOpts, certstorevault.WithMount(cfg.CertificateStore.VaultMount))
			}
			if cfg.CertificateStore.VaultPrefix != "" {
				storeOpts = append(storeOpts, certstorevault.WithPrefix(cfg.CertificateStore.VaultPrefix))
			}
			var err error
			issuer, err = certstorevault.New(issuer, logger, storeOpts...)
			if err != nil {
				logger.Error("failed to create vault certificate store", "error", err)
				os.Exit(1)
			}
		}
		logger.Info("certificate store enabled", "provider", cfg.CertificateStore.Provider)
	}

	// --- Netscaler CPX provisioner ---
	netscalerOpts := []provnetscaler.Option{
		provnetscaler.WithEndpoint(cfg.Provisioner.Endpoint),
		provnetscaler.WithCredentials(cfg.Provisioner.Username, cfg.Provisioner.Password),
	}
	if cfg.Provisioner.InsecureSkipVerify {
		netscalerOpts = append(netscalerOpts, provnetscaler.WithInsecureSkipVerify())
	}

	prov := provnetscaler.New(&provisioner.NetscalerMapper{}, netscalerOpts...)

	// --- Reconciler ---
	desiredState := sourcemongo.NewDesiredState(collection, &source.MongoMapper{})
	rec := reconciler.New(desiredState, issuer, prov, logger)

	reconcileInterval, err := time.ParseDuration(cfg.Reconcile.Interval)
	if err != nil {
		logger.Error("invalid reconcile interval", "value", cfg.Reconcile.Interval, "error", err)
		os.Exit(1)
	}

	// --- Orchestrator ---
	orchOpts := []orchestrator.Option{
		orchestrator.WithReconciler(rec, reconcileInterval),
	}

	// --- Leader Election (optional) ---
	if cfg.LeaderElection.Enabled {
		identity := cfg.LeaderElection.Identity
		if identity == "" {
			identity, _ = os.Hostname()
		}

		leaseDuration, err := time.ParseDuration(cfg.LeaderElection.LeaseDuration)
		if err != nil {
			logger.Error("invalid leader_election.lease_duration", "value", cfg.LeaderElection.LeaseDuration, "error", err)
			os.Exit(1)
		}
		retryPeriod, err := time.ParseDuration(cfg.LeaderElection.RetryInterval)
		if err != nil {
			logger.Error("invalid leader_election.retry_interval", "value", cfg.LeaderElection.RetryInterval, "error", err)
			os.Exit(1)
		}

		var elector corelease.LeaderElector

		switch cfg.LeaderElection.Provider {
		case "kube":
			renewDeadline, err := time.ParseDuration(cfg.LeaderElection.RenewDeadline)
			if err != nil {
				logger.Error("invalid leader_election.renew_deadline", "value", cfg.LeaderElection.RenewDeadline, "error", err)
				os.Exit(1)
			}

			elector = leasekube.New(clientset,
				leasekube.WithNamespace(cfg.LeaderElection.Namespace),
				leasekube.WithLeaseName(cfg.LeaderElection.LeaseName),
				leasekube.WithIdentity(identity),
				leasekube.WithLeaseDuration(leaseDuration),
				leasekube.WithRenewDeadline(renewDeadline),
				leasekube.WithRetryPeriod(retryPeriod),
			)

		case "mongo":
			leaseCol := mongoClient.Database(cfg.Datasource.Database).Collection("leases")
			elector = leasemongo.New(leaseCol,
				leasemongo.WithLeaseName(cfg.LeaderElection.LeaseName),
				leasemongo.WithIdentity(identity),
				leasemongo.WithLeaseDuration(leaseDuration),
				leasemongo.WithRetryInterval(retryPeriod),
			)
		}

		orchOpts = append(orchOpts, orchestrator.WithLeaderElection(elector))
		logger.Info("leader election enabled", "provider", cfg.LeaderElection.Provider, "identity", identity)
	}

	o := orchestrator.New(trig, issuer, prov, logger, orchOpts...)

	logger.Info("routes-provisioner starting",
		"datasource", cfg.Datasource.URI,
		"database", cfg.Datasource.Database,
		"collection", cfg.Datasource.Collection,
		"provisioner", cfg.Provisioner.Endpoint,
		"reconcile_interval", reconcileInterval,
	)

	if err := o.Run(ctx); err != nil {
		logger.Error("orchestrator stopped", "error", err)
		os.Exit(1)
	}
}
