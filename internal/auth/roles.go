package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RoleMapping maps a role ID to its AMS resource type for AccessReview checks
type RoleMapping struct {
	ID          string `yaml:"id"`
	AMSResource string `yaml:"amsResource"`
}

type rolesConfig struct {
	Roles []RoleMapping `yaml:"roles"`
}

// LoadRoles reads role mappings from a YAML config file
func LoadRoles(path string) ([]RoleMapping, error) {
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		return nil, fmt.Errorf("configuration file provided via absolute path. Please provide a relative path instead")
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("unable to determine working directory: %w", err)
	}

	realWd, err := filepath.EvalSymlinks(wd)
	if err != nil {
		return nil, fmt.Errorf("reading roles config %s: %w", path, err)
	}
	realPath, err := filepath.EvalSymlinks(filepath.Join(realWd, cleaned))
	if err != nil {
		return nil, fmt.Errorf("reading roles config %s: %w", path, err)
	}

	rel, err := filepath.Rel(realWd, realPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("roles config path escapes working directory: %s", path)
	}

	data, err := os.ReadFile(realPath)
	if err != nil {
		return nil, fmt.Errorf("reading roles config %s: %w", path, err)
	}

	var cfg rolesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing roles config %s: %w", path, err)
	}

	if len(cfg.Roles) == 0 {
		return nil, fmt.Errorf("no roles defined in %s", path)
	}

	return cfg.Roles, nil
}
