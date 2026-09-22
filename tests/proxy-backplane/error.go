package main

import (
	"encoding/json"
	"net/http"
)

type jsonError struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
}

func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	b, _ := json.Marshal(jsonError{StatusCode: statusCode, Message: message})
	w.Write(b)
}
