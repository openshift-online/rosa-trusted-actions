package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"
)

type Handler struct {
	store      *ActionStore
	logger     *logrus.Logger
	listenAddr string
}

type registerRequest struct {
	Name               string `json:"name"`
	CustomerDataAccess string `json:"customerDataAccess"`
	Kubeconfig         string `json:"kubeconfig"`
}

type registerResponse struct {
	ProxyURI   string `json:"proxyUri"`
	InstanceID string `json:"instanceId"`
}

type statusResponse struct {
	ProxyURI   string `json:"proxyUri"`
	InstanceID string `json:"instanceId"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "cluster_id")

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	if req.Kubeconfig == "" {
		writeJSONError(w, http.StatusBadRequest, "kubeconfig field is required")
		return
	}

	raw, err := base64.StdEncoding.DecodeString(req.Kubeconfig)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "kubeconfig is not valid base64: "+err.Error())
		return
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid kubeconfig: "+err.Error())
		return
	}

	if restCfg.Host == "" {
		writeJSONError(w, http.StatusBadRequest, "kubeconfig missing server URL")
		return
	}

	if restCfg.BearerToken == "" && len(restCfg.CertData) == 0 && restCfg.CertFile == "" {
		writeJSONError(w, http.StatusBadRequest, "kubeconfig missing credentials: need bearer token or client certificate")
		return
	}

	transport, err := buildTransport(restCfg.CAData, restCfg.CertData, restCfg.KeyData)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "failed to build TLS transport: "+err.Error())
		return
	}

	instanceID := fmt.Sprintf("%s--%s", req.Name, uuid.New().String())

	h.store.Put(&ActionEntry{
		ClusterID:  clusterID,
		InstanceID: instanceID,
		Name:       req.Name,
		Transport:  transport,
		Token:      restCfg.BearerToken,
	})

	h.logger.WithFields(logrus.Fields{
		"cluster_id":  clusterID,
		"instance_id": instanceID,
		"name":        req.Name,
	}).Info("Registered trusted action")

	proxyURI := buildProxyURI(h.listenAddr, clusterID, instanceID)
	writeJSON(w, http.StatusOK, registerResponse{
		ProxyURI:   proxyURI,
		InstanceID: instanceID,
	})
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "cluster_id")
	instanceID := chi.URLParam(r, "instanceId")

	entry, ok := h.store.Get(clusterID, instanceID)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "trusted action instance not found")
		return
	}

	proxyURI := buildProxyURI(h.listenAddr, entry.ClusterID, entry.InstanceID)
	writeJSON(w, http.StatusOK, statusResponse{
		ProxyURI:   proxyURI,
		InstanceID: entry.InstanceID,
	})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "cluster_id")
	instanceID := chi.URLParam(r, "instanceId")

	if !h.store.Delete(clusterID, instanceID) {
		writeJSONError(w, http.StatusNotFound, "trusted action instance not found")
		return
	}

	h.logger.WithFields(logrus.Fields{
		"cluster_id":  clusterID,
		"instance_id": instanceID,
	}).Info("Deleted trusted action")

	w.WriteHeader(http.StatusNoContent)
}

func buildTransport(caData, certData, keyData []byte) (*http.Transport, error) {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if len(caData) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.RootCAs = pool
	}

	if len(certData) > 0 && len(keyData) > 0 {
		cert, err := tls.X509KeyPair(certData, keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return &http.Transport{
		TLSClientConfig: tlsCfg,
	}, nil
}

func buildProxyURI(listenAddr, clusterID, instanceID string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		host = listenAddr
		port = "8080"
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s/backplane/trustedaction/%s/%s", host, port, clusterID, instanceID)
}

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}
