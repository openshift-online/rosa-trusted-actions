package rta

import (
	"fmt"

	ocmconfig "github.com/openshift-online/ocm-cli/pkg/config"
	"github.com/spf13/cobra"
)

// resolveToken returns a bearer token using the precedence:
// --token flag > OCM config file access token.
func (g *commandGroup) resolveToken(cmd *cobra.Command) (string, error) {
	if t := fromFlag(cmd, "token"); t != "" {
		return t, nil
	}

	cfg, err := ocmconfig.Load()
	if err != nil {
		return "", fmt.Errorf("load OCM configuration: %w", err)
	}
	if cfg == nil {
		return "", fmt.Errorf("not authenticated: use --token or log in with ocm")
	}
	if cfg.AccessToken == "" {
		return "", fmt.Errorf("not authenticated: OCM config has no access token; log in with ocm")
	}

	// TODO: use the OCM SDK to refresh an expired access token when a
	// valid refresh token is available, instead of sending an expired JWT.
	// OR: use rosactl auth once this becomes a plugin/part of rosactl
	armed, reason, err := cfg.Armed(false)
	if err != nil {
		return "", fmt.Errorf("check OCM token: %w", err)
	}
	if !armed {
		return "", fmt.Errorf("OCM token expired: %s; log in again with ocm", reason)
	}

	return cfg.AccessToken, nil
}
