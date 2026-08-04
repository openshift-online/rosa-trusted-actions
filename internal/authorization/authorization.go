package authorization

import "github.com/sirupsen/logrus"

type Result struct {
	Allowed bool
	Reason  string
}

type Request struct {
	Namespace     string
	ResourceType  string
	ResourceName  string
	ClusterScoped bool
}

type Authorizer interface {
	Authorize(req Request) Result
}

type authorizer struct {
	logger            *logrus.Logger
	allowedNamespaces map[string]bool
	allowedSecrets    map[string]bool
}

func New(logger *logrus.Logger, allowedNamespaces []string, allowedSecrets []string) Authorizer {
	nsSet := make(map[string]bool, len(allowedNamespaces))
	for _, ns := range allowedNamespaces {
		nsSet[ns] = true
	}
	secSet := make(map[string]bool, len(allowedSecrets))
	for _, s := range allowedSecrets {
		secSet[s] = true
	}
	return &authorizer{
		logger:            logger,
		allowedNamespaces: nsSet,
		allowedSecrets:    secSet,
	}
}

func (a *authorizer) Authorize(req Request) Result {
	// Permissive mode: when no namespace allowlist is configured (e.g. local dev),
	// allow all requests so that cluster-scoped and unconfigured environments work.
	// In production ROSA_TA_ALLOWED_NAMESPACES must be set to enforce restrictions.
	if len(a.allowedNamespaces) == 0 {
		a.logger.Debug("no namespace allowlist configured — operating in permissive mode")
		return Result{Allowed: true, Reason: "permissive (no namespace allowlist configured)"}
	}

	if req.Namespace == "" {
		return Result{Allowed: false, Reason: "namespace is required"}
	}

	if req.Namespace == "" && !req.ClusterScoped {
		return Result{Allowed: false, Reason: "namespace is required for namespaced resources"}
	}

	if req.Namespace != "" && !a.allowedNamespaces[req.Namespace] {
		a.logger.WithField("namespace", req.Namespace).Debug("namespace not in allowlist")
		return Result{Allowed: false, Reason: "namespace not in allowlist: " + req.Namespace}
	}

	if req.ResourceType == "secrets" {
		key := req.Namespace + "/" + req.ResourceName
		if req.ResourceName == "" || !a.allowedSecrets[key] {
			a.logger.WithField("secret", key).Debug("secret access denied")
			return Result{Allowed: false, Reason: "secrets access denied: " + key + " not in allowlist"}
		}
	}

	return Result{Allowed: true, Reason: "authorized"}
}
