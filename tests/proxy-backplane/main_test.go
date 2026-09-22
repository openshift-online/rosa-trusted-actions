package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServerStarts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := newRootCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--listen-addr", addr, "--log-level", "warn"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("server exited early: %v", err)
		default:
		}
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp.Body.Close()
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server did not accept connections within 2s: %v", lastErr)
}
