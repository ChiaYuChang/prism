package main

import (
	"time"

	"github.com/spf13/cobra"
)

func newRootAuthCommand(ctx *cliContext) *cobra.Command {
	cmd := &cobra.Command{Use: "root", Short: "Root break-glass commands"}
	cmd.AddCommand(rootInitCommand(ctx), rootCheckCommand(ctx), rootAdminCreateCommand(ctx), rootTokensRevokeAllCommand(ctx))
	return cmd
}

func rootInitCommand(ctx *cliContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize root token fingerprint",
		RunE: func(cmd *cobra.Command, args []string) error {
			cred, err := ctx.rootCredential()
			if err != nil {
				return err
			}
			service, err := ctx.service(cmd.Context())
			if err != nil {
				return err
			}
			defer ctx.close()
			tok, err := service.InitRoot(cmd.Context(), cred.Secret)
			if err != nil {
				return renderError(ctx, "root", "init", err, cred.Warnings)
			}
			return render(ctx, "root", "init", toTokenView(tok), cred.Warnings)
		},
	}
	return cmd
}

func rootCheckCommand(ctx *cliContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check root token against database fingerprint",
		RunE: func(cmd *cobra.Command, args []string) error {
			cred, err := ctx.rootCredential()
			if err != nil {
				return err
			}
			service, err := ctx.service(cmd.Context())
			if err != nil {
				return err
			}
			defer ctx.close()
			if err := service.CheckRoot(cmd.Context(), cred.Secret); err != nil {
				return renderError(ctx, "root", "check", err, cred.Warnings)
			}
			return render(ctx, "root", "check", map[string]bool{"valid": true}, cred.Warnings)
		},
	}
	return cmd
}

func rootAdminCreateCommand(ctx *cliContext) *cobra.Command {
	var name string
	var expiresAt string
	cmd := &cobra.Command{
		Use:   "admin-create",
		Short: "Create an admin token with root token",
		RunE: func(cmd *cobra.Command, args []string) error {
			cred, err := ctx.rootCredential()
			if err != nil {
				return err
			}
			expires, err := parseOptionalTime(expiresAt)
			if err != nil {
				return err
			}
			service, err := ctx.service(cmd.Context())
			if err != nil {
				return err
			}
			defer ctx.close()
			res, err := service.CreateAdminWithRoot(cmd.Context(), cred.Secret, name, expires)
			if err != nil {
				return renderError(ctx, "root", "admin_create", err, cred.Warnings)
			}
			return render(ctx, "root", "admin_create", toTokenSecretView(res.Token, res.Raw), cred.Warnings)
		},
	}
	cmd.Flags().StringVar(&name, "name", "initial-admin", "Admin token name")
	cmd.Flags().StringVar(&expiresAt, "expires-at", "", "Token expiry RFC3339 timestamp")
	return cmd
}

func rootTokensRevokeAllCommand(ctx *cliContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens-revoke-all",
		Short: "Revoke all non-root tokens with root token",
		RunE: func(cmd *cobra.Command, args []string) error {
			cred, err := ctx.rootCredential()
			if err != nil {
				return err
			}
			service, err := ctx.service(cmd.Context())
			if err != nil {
				return err
			}
			defer ctx.close()
			count, err := service.RevokeAllWithRoot(cmd.Context(), cred.Secret)
			if err != nil {
				return renderError(ctx, "root", "tokens_revoke_all", err, cred.Warnings)
			}
			return render(ctx, "root", "tokens_revoke_all", map[string]int64{"revoked": count}, cred.Warnings)
		},
	}
	return cmd
}

func parseOptionalTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
