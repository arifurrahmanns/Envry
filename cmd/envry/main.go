// Command envry is a local email testing server: SMTP capture, POP3, IMAP
// and a web UI in one binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/arifurrahmanns/envry/internal/app"
	"github.com/arifurrahmanns/envry/internal/config"
	"github.com/arifurrahmanns/envry/internal/version"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "envry:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "envry",
		Short: "Local email testing server: SMTP capture, POP3, IMAP and a web UI",
		Long: `Envry captures every email your app sends over SMTP, keeps it local,
and serves it back over POP3, IMAP, a REST API and a web UI.

Running "envry" with no command is the same as "envry serve".
Every flag has an ENVRY_* environment variable; flags win over env.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Server flags are persistent so `envry --smtp ...` and later commands
	// such as `envry send` share them.
	flags := config.Register(root.PersistentFlags())

	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run the SMTP, POP3, IMAP and HTTP servers (default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, flags)
		},
	}
	root.RunE = serve.RunE

	root.AddCommand(serve, newSendCmd(flags), newVersionCmd())
	return root
}

func runServe(cmd *cobra.Command, flags *config.Flags) error {
	cfg, err := flags.Load(cmd.Flags(), os.LookupEnv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	log.Info("starting envry", "version", version.Version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, cfg, log)
	if err != nil {
		return err
	}
	if err := a.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "envry %s (commit %s, built %s, %s %s/%s)\n",
				version.Version, version.Commit, version.Date,
				runtime.Version(), runtime.GOOS, runtime.GOARCH)
		},
	}
}
