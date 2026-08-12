package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	sdk "github.com/openshift-online/ocm-sdk-go"
	"github.com/openshift-online/ocm-sdk-go/authentication"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift-online/rosa-trusted-actions/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions/internal/catalog"
	"github.com/openshift-online/rosa-trusted-actions/internal/config"
	"github.com/openshift-online/rosa-trusted-actions/internal/handlers"
	"github.com/openshift-online/rosa-trusted-actions/internal/middleware"
	"github.com/openshift-online/rosa-trusted-actions/internal/ocm"
	"github.com/openshift-online/rosa-trusted-actions/internal/openapi"
	"github.com/openshift-online/rosa-trusted-actions/internal/store"
	"github.com/openshift-online/rosa-trusted-actions/internal/worker"
)

var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rosa-trusted-actions-server",
		Short: "ROSA Trusted Actions Server",
		Long:  "HTTP server for the ROSA Trusted Actions API",
		RunE:  runServer,
	}

	// Add server flags
	cmd.Flags().String("listen-addr", ":8080", "listen address")
	cmd.Flags().String("log-level", "info", "log level (debug, info, warn, error)")
	cmd.Flags().Bool("log-json", false, "enable JSON logging")

	return cmd
}

func runServer(cmd *cobra.Command, args []string) error {
	// Get CLI flag values
	listenAddr, _ := cmd.Flags().GetString("listen-addr")
	logLevel, _ := cmd.Flags().GetString("log-level")
	logJSON, _ := cmd.Flags().GetBool("log-json")

	// Load configuration from environment
	cfg := config.Load()

	// Override with CLI flags
	cfg.ListenAddr = listenAddr
	cfg.LogLevel = logLevel
	cfg.LogJSON = logJSON

	// Setup logging
	logger := setupLogging(cfg)

	// Initialize database
	dataStore, err := store.NewSQLiteStore(cmd.Context(), cfg.DatabaseURL, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize database")
	}
	defer func() {
		if err := dataStore.Close(); err != nil {
			logger.WithError(err).Error("Failed to close database")
		}
	}()

	// -------------------------------------------------------------------------
	// Worker pool — dequeues pending executions and runs them in the
	// background, off the HTTP request path.
	// -------------------------------------------------------------------------
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	workerPool := worker.New(dataStore, logger, worker.NewNoopRunner(logger), cfg.WorkerConcurrency, cfg.WorkerPollInterval)
	workerPool.Start(workerCtx)

	// Create handler implementation
	actionCatalog := catalog.New()
	apiHandler := handlers.NewAPIHandler(logger, actionCatalog, dataStore, workerPool)

	// Setup auth middleware
	var authnMiddleware auth.JWTMiddleware
	var authzMiddleware auth.AuthorizationMiddleware

	if cfg.EnableAuth {
		authnMiddleware = auth.NewAuthMiddleware(logger)

		roles, err := auth.LoadRoles(cfg.RolesConfigPath)
		if err != nil {
			logger.WithError(err).Fatal("Failed to load role configuration")
		}

		ocmClient, err := ocm.NewClient(ocm.Config{
			BaseURL:      cfg.OCMBaseURL,
			ClientID:     cfg.OCMClientID,
			ClientSecret: cfg.OCMClientSecret,
			SelfToken:    cfg.OCMToken,
		})
		if err != nil {
			logger.WithError(err).Fatal("Failed to create OCM client")
		}
		defer func() {
			if err := ocmClient.Close(); err != nil {
				logger.WithError(err).Error("Failed to close OCM connection")
			}
		}()

		authzMiddleware = auth.NewRoleAuthzMiddleware(roles, ocmClient.Authorization, logger)
	} else {
		logger.Warn("Auth disabled — using mock identity 'dev-user' with SREP role. Do not use in production.")
		authnMiddleware = auth.NewMockAuthMiddleware()
		authzMiddleware = auth.NewMockAuthzMiddleware(logger)
	}

	// Safety guard: mock auth + real backplane is a dangerous misconfiguration.
	// An unauthenticated request would receive the hardcoded SREP role and reach
	// production cluster infrastructure via the backplane provider. Require an
	// explicit kubeconfig so the real backplane is never reached without auth.
	if !cfg.EnableAuth && cfg.Kubeconfig == "" {
		logger.Fatal("ROSA_TA_ENABLE_AUTH=false requires ROSA_TA_KUBECONFIG to be set; " +
			"running mock auth against the real backplane is not permitted")
	}

	// -------------------------------------------------------------------------
	// Handler and router
	// -------------------------------------------------------------------------
	actionAuthz := auth.NewActionAuthzMiddleware(apiHandler.ActionCatalog, logger)

	router := chi.NewRouter()

	// Global middleware
	router.Use(middleware.NewLogger(logger))
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(chimiddleware.RealIP)
	router.Use(chimiddleware.Timeout(60 * time.Second))

	// CORS configuration
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Amz-Date", "X-Amz-Security-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check endpoint (no auth required)
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","version":"%s","build_date":"%s","git_commit":"%s"}`, version, buildDate, gitCommit)
	})

	// API routes — authn and authz applied as chi middleware so the entire
	// sub-tree is protected. HandlerWithOptions registers directly on r via
	// BaseRouter to avoid an infinite routing loop from r.Mount("/", r).
	router.Route("/api/v0/trusted-actions", func(r chi.Router) {
		r.Use(authnMiddleware.AuthenticateAccountJWT)
		r.Use(middleware.NewAuditLogger(dataStore, logger))
		r.Use(authzMiddleware.AuthorizeAPI)
		openapi.HandlerWithOptions(apiHandler, openapi.ChiServerOptions{
			BaseRouter:  r,
			Middlewares: []openapi.MiddlewareFunc{actionAuthz.CheckActionAccess},
		})
	})

	// -------------------------------------------------------------------------
	// JWKS wrapper — only applied when real auth is enabled.
	// The OCM SDK handler validates JWT signatures against the JWKS endpoint and
	// stores the verified token in context; auth.Middleware then extracts claims.
	// Skipping this in mock mode avoids a network call and a Fatal at startup.
	// -------------------------------------------------------------------------
	var mainHandler http.Handler = router

	if cfg.EnableAuth {
		authnLogger, err := sdk.NewStdLoggerBuilder().
			Debug(logger.Level >= logrus.DebugLevel).
			Build()
		if err != nil {
			logger.WithError(err).Fatal("Failed to create OCM authentication logger")
		}

		builder := authentication.NewHandler().
			Logger(authnLogger).
			KeysURL(cfg.JWKCertURL).
			Public("^/health$").
			Next(mainHandler)

		if cfg.JWKCertFile != "" {
			builder = builder.KeysFile(cfg.JWKCertFile)
		}

		mainHandler, err = builder.Build()
		if err != nil {
			logger.WithError(err).Fatal("Failed to build OCM authentication handler")
		}
	}

	// -------------------------------------------------------------------------
	// HTTP server
	// -------------------------------------------------------------------------
	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mainHandler,
		// Security settings
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	go func() {
		logger.WithField("addr", cfg.ListenAddr).Info("Starting server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Failed to start server")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	cancelWorkers()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Server forced to shutdown")
		return err
	}

	workersDone := make(chan struct{})
	go func() {
		workerPool.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-ctx.Done():
		logger.Warn("Timed out waiting for worker pool to finish in-flight executions")
	}

	logger.Info("Server stopped")
	return nil
}

func setupLogging(cfg *config.Config) *logrus.Logger {
	logger := logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// Set JSON format if requested
	if cfg.LogJSON {
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339,
		})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339,
		})
	}

	return logger
}
