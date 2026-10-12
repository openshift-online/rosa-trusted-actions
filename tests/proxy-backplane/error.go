package main

import (
	"encoding/json"
	"net/http"

	"github.com/sirupsen/logrus"
)

type jsonError struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
}

func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	b, err := json.Marshal(jsonError{StatusCode: statusCode, Message: message})
	if err != nil {
		logrus.WithError(err).Error("failed to marshal JSON error response")
		return
	}
	if _, err := w.Write(b); err != nil {
		logrus.WithError(err).Error("failed to write JSON error response")
	}
}
