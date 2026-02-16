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

	certacmehttp "github.com/nicol/dynamic-route-provisioner/cert-acme-http"
	leasekube "github.com/nicol/dynamic-route-provisioner/lease-kube"
	provnetscaler "github.com/nicol/dynamic-route-provisioner/provisioner-netscaler"
	triggermongo "github.com/nicol/dynamic-route-provisioner/trigger-mongo"

	"github.com/nicol/dynamic-route-provisioner/sds-provisioner/internal/certificate"
	"github.com/nicol/dynamic-route-provisioner/sds-provisioner/internal/config"
	"github.com/nicol/dynamic-route-provisioner/sds-provisioner/internal/provisioner"
	"github.com/nicol/dynamic-route-provisioner/sds-provisioner/internal/trigger"

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
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoDB.URI))
	if err != nil {
		logger.Error("failed to connect to mongodb", "error", err)
		os.Exit(1)
	}
	defer mongoClient.Disconnect(ctx)

	collection := mongoClient.Database(cfg.MongoDB.Database).Collection(cfg.MongoDB.Collection)
	trig := triggermongo.New(collection, &trigger.WorkspaceMapper{})

	// --- ACME HTTP-01 certificate issuer ---
	challengeServer := certificate.NewChallengeServer(cfg.ACME.ChallengePort)

	acmeOpts := []certacmehttp.Option{
		certacmehttp.WithEmail(cfg.ACME.Email),
	}
	if cfg.ACME.DirectoryURL != "" {
		acmeOpts = append(acmeOpts, certacmehttp.WithDirectoryURL(cfg.ACME.DirectoryURL))
	}

	issuer, err := certacmehttp.New(challengeServer, acmeOpts...)
	if err != nil {
		logger.Error("failed to create acme issuer", "error", err)
		os.Exit(1)
	}

	// --- Netscaler CPX provisioner ---
	netscalerOpts := []provnetscaler.Option{
		provnetscaler.WithEndpoint(cfg.Netscaler.Endpoint),
		provnetscaler.WithCredentials(cfg.Netscaler.Username, cfg.Netscaler.Password),
	}
	if cfg.Netscaler.InsecureSkipVerify {
		netscalerOpts = append(netscalerOpts, provnetscaler.WithInsecureSkipVerify())
	}

	prov := provnetscaler.New(&provisioner.NetscalerMapper{}, netscalerOpts...)

	// --- Reconciler ---
	desiredState := triggermongo.NewDesiredState(collection, &trigger.WorkspaceMapper{})
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
		renewDeadline, err := time.ParseDuration(cfg.LeaderElection.RenewDeadline)
		if err != nil {
			logger.Error("invalid leader_election.renew_deadline", "value", cfg.LeaderElection.RenewDeadline, "error", err)
			os.Exit(1)
		}
		retryPeriod, err := time.ParseDuration(cfg.LeaderElection.RetryInterval)
		if err != nil {
			logger.Error("invalid leader_election.retry_interval", "value", cfg.LeaderElection.RetryInterval, "error", err)
			os.Exit(1)
		}

		kubeConfig, err := rest.InClusterConfig()
		if err != nil {
			logger.Error("failed to get in-cluster config", "error", err)
			os.Exit(1)
		}
		clientset, err := kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			logger.Error("failed to create kubernetes client", "error", err)
			os.Exit(1)
		}

		elector := leasekube.New(clientset,
			leasekube.WithNamespace(cfg.LeaderElection.Namespace),
			leasekube.WithLeaseName(cfg.LeaderElection.LeaseName),
			leasekube.WithIdentity(identity),
			leasekube.WithLeaseDuration(leaseDuration),
			leasekube.WithRenewDeadline(renewDeadline),
			leasekube.WithRetryPeriod(retryPeriod),
		)

		orchOpts = append(orchOpts, orchestrator.WithLeaderElection(elector))
		logger.Info("leader election enabled", "identity", identity, "namespace", cfg.LeaderElection.Namespace)
	}

	o := orchestrator.New(trig, issuer, prov, logger, orchOpts...)

	logger.Info("sds-provisioner starting",
		"mongodb", cfg.MongoDB.URI,
		"database", cfg.MongoDB.Database,
		"collection", cfg.MongoDB.Collection,
		"netscaler", cfg.Netscaler.Endpoint,
		"reconcile_interval", reconcileInterval,
	)

	if err := o.Run(ctx); err != nil {
		logger.Error("orchestrator stopped", "error", err)
		os.Exit(1)
	}
}
