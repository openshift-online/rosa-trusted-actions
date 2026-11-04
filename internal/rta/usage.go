package rta

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// invalidUsage wraps a usage error so the root prints the command's usage and
// examples on stderr.
func invalidUsage(cmd *cobra.Command, format string, args ...any) error {
	return &usageError{cmd: cmd, msg: fmt.Sprintf(format, args...)}
}

type usageError struct {
	cmd *cobra.Command
	msg string
}

func (e *usageError) Error() string { return e.msg }

func PrintUsageIfUsageError(err error) {
	var ue *usageError
	if !errors.As(err, &ue) || ue.cmd == nil {
		return
	}
	fmt.Fprintf(ue.cmd.ErrOrStderr(), "Usage:\n  %s\n", ue.cmd.UseLine())
	if ue.cmd.HasExample() {
		fmt.Fprintf(ue.cmd.ErrOrStderr(), "\nExamples:\n%s\n", ue.cmd.Example)
	}
}
