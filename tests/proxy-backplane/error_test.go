package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSONError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
	}{
		{"not found", http.StatusNotFound, "trusted action instance not found"},
		{"bad request", http.StatusBadRequest, "malformed kubeconfig"},
		{"internal error", http.StatusInternalServerError, "unexpected failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSONError(w, tt.statusCode, tt.message)

			if w.Code != tt.statusCode {
				t.Errorf("got status %d, want %d", w.Code, tt.statusCode)
			}

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("got Content-Type %q, want %q", ct, "application/json")
			}

			var body jsonError
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response body: %v", err)
			}
			if body.StatusCode != tt.statusCode {
				t.Errorf("got body statusCode %d, want %d", body.StatusCode, tt.statusCode)
			}
			if body.Message != tt.message {
				t.Errorf("got body message %q, want %q", body.Message, tt.message)
			}
		})
	}
}
