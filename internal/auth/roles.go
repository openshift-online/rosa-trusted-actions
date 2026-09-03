package auth

import (
	"fmt"
	"os"
	"path/filepath"

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
	if !filepath.IsAbs(cleaned) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("unable to determine working directory: %w", err)
		}

		realWd, err := filepath.EvalSymlinks(wd)
		if err != nil {
			return nil, fmt.Errorf("reading roles config %s: %w", path, err)
		}
		cleaned, err = filepath.EvalSymlinks(filepath.Join(realWd, cleaned))
		if err != nil {
			return nil, fmt.Errorf("reading roles config %s: %w", path, err)
		}
	}

	data, err := os.ReadFile(cleaned)
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
