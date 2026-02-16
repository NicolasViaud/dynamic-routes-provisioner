package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nicol/dynamic-route-provisioner/core/orchestrator"

	certacmehttp "github.com/nicol/dynamic-route-provisioner/cert-acme-http"
	provnetscaler "github.com/nicol/dynamic-route-provisioner/provisioner-netscaler"
	triggermongo "github.com/nicol/dynamic-route-provisioner/trigger-mongo"

	"github.com/nicol/dynamic-route-provisioner/sds-provisioner/internal/certificate"
	"github.com/nicol/dynamic-route-provisioner/sds-provisioner/internal/config"
	"github.com/nicol/dynamic-route-provisioner/sds-provisioner/internal/provisioner"
	"github.com/nicol/dynamic-route-provisioner/sds-provisioner/internal/trigger"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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

	// --- Orchestrator ---
	o := orchestrator.New(trig, issuer, prov, logger)

	logger.Info("sds-provisioner starting",
		"mongodb", cfg.MongoDB.URI,
		"database", cfg.MongoDB.Database,
		"collection", cfg.MongoDB.Collection,
		"netscaler", cfg.Netscaler.Endpoint,
	)

	if err := o.Run(ctx); err != nil {
		logger.Error("orchestrator stopped", "error", err)
		os.Exit(1)
	}
}
