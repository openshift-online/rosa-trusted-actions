package rta

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Overridden at build time via:
//
//	-X github.com/openshift-online/rosa-trusted-actions/internal/rta.Version=...
//	-X github.com/openshift-online/rosa-trusted-actions/internal/rta.Commit=...
//	-X github.com/openshift-online/rosa-trusted-actions/internal/rta.BuildDate=...
var (
	Version   = "dev"
	Commit    = ""
	BuildDate = ""
)

type versionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

func getVersionInfo() versionInfo {
	/* If this ever becomes a rosactl plugin, use:
	if hostVersion := os.Getenv("ROSACTL_HOST_VERSION"); hostVersion != "" {
		ua += " rosactl/" + hostVersion
	} */
	v := Version
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && strings.HasPrefix(info.Main.Version, "v") {
			v = info.Main.Version
		}
	}
	c := Commit
	if c == "" {
		c = "unknown"
	}
	d := BuildDate
	if d == "" {
		d = "unknown"
	}
	return versionInfo{
		Version:   v,
		Commit:    c,
		BuildDate: d,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func (i versionInfo) String() string {
	return fmt.Sprintf("rta version: %s\ncommit: %s\nbuild date: %s\ngo: %s\nplatform: %s",
		i.Version, i.Commit, i.BuildDate, i.GoVersion, i.Platform)
}

func versionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Short:   "Show the rta version",
		Example: "  rta version\n  rta version -o json",
		Args:    rejectUnknownArgsAndSuggestSimilar,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateFormat(cmd); err != nil {
				return err
			}
			info := getVersionInfo()
			if fromFlag(cmd, "output") == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(info)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), info.String())
			return err
		},
	}
	addOutputFormatFlag(cmd)
	return cmd
}
