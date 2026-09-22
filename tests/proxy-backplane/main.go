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
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy-backplane",
		Short: "Mock backplane proxy for trusted action integration tests",
		RunE:  run,
	}

	cmd.Flags().String("listen-addr", ":8080", "listen address")
	cmd.Flags().String("log-level", "info", "log level (debug, info, warn, error)")

	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	listenAddr, _ := cmd.Flags().GetString("listen-addr")
	logLevel, _ := cmd.Flags().GetString("log-level")

	logger := logrus.New()
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})

	store := NewActionStore()
	handler := &Handler{
		store:      store,
		logger:     logger,
		listenAddr: listenAddr,
	}

	router := chi.NewRouter()
	router.Post("/backplane/trustedactions/{cluster_id}", handler.Register)
	router.Get("/backplane/trustedactions/{cluster_id}/{instanceId}", handler.Status)
	router.Delete("/backplane/trustedactions/{cluster_id}/{instanceId}", handler.Delete)

	srv := &http.Server{
		Addr:           listenAddr,
		Handler:        router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		logger.WithField("addr", listenAddr).Info("Starting proxy-backplane server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Failed to start server")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
	case <-cmd.Context().Done():
	}

	logger.Info("Shutting down server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("Server forced to shutdown")
		return err
	}

	logger.Info("Server stopped")
	return nil
}
