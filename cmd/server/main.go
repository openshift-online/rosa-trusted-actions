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
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift-online/rosa-trusted-actions-server/internal/openapi"
	"github.com/openshift-online/rosa-trusted-actions-server/internal/config"
	"github.com/openshift-online/rosa-trusted-actions-server/internal/handlers"
	"github.com/openshift-online/rosa-trusted-actions-server/internal/middleware"
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
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Amz-Date", "X-Amz-Security-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check endpoint
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","version":"%s","build_date":"%s","git_commit":"%s"}`, version, buildDate, gitCommit)
	})

	// Add API routes with base path
	router.Route("/api/v0/trusted-actions", func(r chi.Router) {
		r.Mount("/", openapi.HandlerFromMux(apiHandler, r))
	})

	// Create server
	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: router,
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
