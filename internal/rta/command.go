// Package rta implements the ROSA Trusted Actions client command group.
package rta

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultTimeout = 5 * time.Minute

// NewCommand returns a command tree decoupled from global state like this in
// order to already be compliant for rta to become a rosactl plugin
func NewCommand() *cobra.Command {
	config := viper.New()
	config.SetEnvPrefix("RTA")
	config.AutomaticEnv()
	config.SetDefault("timeout", defaultTimeout)

	root := &cobra.Command{
		Use:   "rta",
		Short: "ROSA Trusted Actions client",
		Long:  "Client for the ROSA Trusted Actions API. List, describe, and execute trusted actions on ROSA clusters.",
		Example: `  rta action list
  rta action describe get
  rta action run get --cluster 1234 --jira OHSS-1234 --param resource=pods --param namespace=openshift-monitoring
  rta execution list
  rta version`,
		Args:          rejectUnknownArgsAndSuggestSimilar,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return invalidUsage(cmd, "%s", err)
	})

	root.PersistentFlags().Bool("verbose", false, "Show debug diagnostics")
	root.PersistentFlags().Duration("timeout", defaultTimeout, "Deadline for non-interactive operations")
	root.PersistentFlags().String("server", "", "RTA API server URL (env: RTA_SERVER)")
	root.PersistentFlags().String("token", "", "Bearer token for authentication (default: OCM config)")

	_ = config.BindPFlag("verbose", root.PersistentFlags().Lookup("verbose"))
	_ = config.BindPFlag("timeout", root.PersistentFlags().Lookup("timeout"))
	_ = config.BindPFlag("server", root.PersistentFlags().Lookup("server"))

	g := &commandGroup{
		config:  config,
		timeout: config.GetDuration,
		verbose: config.GetBool,
		explicitTimeout: func() bool {
			return root.PersistentFlags().Changed("timeout")
		},
	}

	root.AddCommand(
		g.actionCommands(),
		g.executionCommands(),
		g.auditCommands(),
		versionCommand(),
	)

	return root
}

type commandGroup struct {
	config          *viper.Viper
	timeout         func(string) time.Duration
	explicitTimeout func() bool
	verbose         func(string) bool
}

func (g *commandGroup) newClient(cmd *cobra.Command, requireAuth bool) (*Client, error) {
	var opts []ClientOption
	if requireAuth {
		token, err := g.resolveToken(cmd)
		if err != nil {
			return nil, err
		}
		opts = append(opts, WithToken(token))
	} else if t := fromFlag(cmd, "token"); t != "" {
		opts = append(opts, WithToken(t))
	}
	return NewClient(g.config.GetString("server"), opts...)
}

func fromFlag(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func addOutputFormatFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("output", "o", "text", "Result format: text or json")
	_ = cmd.RegisterFlagCompletionFunc("output", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func validateFormat(cmd *cobra.Command) error {
	if f := fromFlag(cmd, "output"); f != "text" && f != "json" {
		return invalidUsage(cmd, "invalid --output %q: expected text or json", f)
	}
	return nil
}

func parent(use, short, long, example string) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Example: example,
		Args:    rejectUnknownArgsAndSuggestSimilar,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
}

func rejectUnknownArgsAndSuggestSimilar(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if cmd.HasAvailableSubCommands() {
		msg := fmt.Sprintf("%q is not a valid subcommand of %s", args[0], cmd.CommandPath())
		cmd.SuggestionsMinimumDistance = 2
		if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) == 1 {
			msg += fmt.Sprintf("; did you mean %q?", suggestions[0])
		}
		return invalidUsage(cmd, "%s", msg)
	}
	return invalidUsage(cmd, "unexpected positional arguments: this command takes none")
}

// requiredArgs returns a Cobra Args validator that names each missing argument.
func requiredArgs(names ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < len(names) {
			return invalidUsage(cmd, "missing required argument: %s", names[len(args)])
		}
		if len(args) > len(names) {
			return invalidUsage(cmd, "unexpected argument: %s", args[len(names)])
		}
		return nil
	}
}
