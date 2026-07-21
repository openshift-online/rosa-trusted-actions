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

	"github.com/openshift-online/rosa-trusted-actions-server/internal/auth"
	"github.com/openshift-online/rosa-trusted-actions-server/internal/config"
	"github.com/openshift-online/rosa-trusted-actions-server/internal/handlers"
	"github.com/openshift-online/rosa-trusted-actions-server/internal/middleware"
	"github.com/openshift-online/rosa-trusted-actions-server/internal/ocm"
	"github.com/openshift-online/rosa-trusted-actions-server/internal/openapi"
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

	// Create handler implementation
	apiHandler := handlers.NewAPIHandler(logger)

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
		defer ocmClient.Close()

		authzMiddleware = auth.NewRoleAuthzMiddleware(roles, ocmClient.Authorization, logger)
		logger.Info("Auth enabled: JWT validation + AMS role resolution")
	} else {
		authnMiddleware = auth.NewMockAuthMiddleware(logger)
		authzMiddleware = auth.NewMockAuthzMiddleware(logger)
		logger.Warn("Auth disabled: using mock authentication (X-Mock-Username + X-Mock-Role headers required)")
	}

	actionAuthz := auth.NewActionAuthzMiddleware(apiHandler.ActionCatalog, logger)

	// Setup router
	router := chi.NewRouter()

	// Add middleware
	router.Use(middleware.NewLogger(logger))
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(chimiddleware.RealIP)
	router.Use(chimiddleware.Timeout(60 * time.Second))

	// CORS configuration
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Amz-Date", "X-Amz-Security-Token", "X-Mock-Username", "X-Mock-Email", "X-Mock-ClientID", "X-Mock-Role"},
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

	// Add API routes with auth middleware
	router.Route("/api/v0/trusted-actions", func(r chi.Router) {
		r.Use(authnMiddleware.AuthenticateAccountJWT)
		r.Use(authzMiddleware.AuthorizeAPI)
		// HandlerWithOptions registers routes directly on r via BaseRouter,
		// so the return value is not mounted — that would cause an infinite
		// routing loop since r.Mount("/", r) re-enters the same router.
		openapi.HandlerWithOptions(apiHandler, openapi.ChiServerOptions{
			BaseRouter:  r,
			Middlewares: []openapi.MiddlewareFunc{actionAuthz.CheckActionAccess},
		})
	})

	// Wrap the router with OCM JWT validation when auth is enabled.
	// This matches the rh-trex pattern (api_server.go:53-74): the OCM SDK handler
	// validates JWT signatures against JWKS, stores the verified token in context,
	// then the auth.Middleware extracts claims from that token.
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

	// Create server
	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mainHandler,
		// Security settings
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	// Start server in goroutine
	go func() {
		logger.WithField("addr", cfg.ListenAddr).Info("Starting server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Failed to start server")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Server forced to shutdown")
		return err
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
