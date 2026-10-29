package main

import (
	"fmt"
	"os"

	"github.com/openshift-online/rosa-trusted-actions/internal/rta"
)

func main() {
	cmd := rta.NewCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n\n", err)
		rta.PrintUsageIfUsageError(err)
		os.Exit(1)
	}
}