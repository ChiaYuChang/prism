package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	cmd := newRootCommand()
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		if !errors.Is(err, errAlreadyRendered) {
			_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	ctx := newCLIContext()
	cmd := &cobra.Command{
		Use:          "prismctl",
		Short:        "Prism operator CLI",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ctx.inputMode == "json" || ctx.inputFile != "" {
				return ctx.runJSON(cmd.Context())
			}
			return cmd.Help()
		},
	}
	ctx.bindPersistentFlags(cmd)
	cmd.AddCommand(newRootAuthCommand(ctx), newAdminCommand(ctx))
	return cmd
}
