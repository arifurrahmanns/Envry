// Command envry is a local email testing server: SMTP capture, POP3, IMAP
// and a web UI in one binary.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
	root := newRootCmd()
	err := root.Execute()
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "envry:", err)
	if h := hint(err); h != "" {
		fmt.Fprintln(os.Stderr, h)
	}
	// A supervising app gives Envry a hidden console of its own, so
	// ownsConsole is true there too; it must not wait for a key press.
	if ownsConsole() && !managed(root, os.LookupEnv) {
		// Started by double-click: keep the window open so the error can be read.
		fmt.Fprint(os.Stderr, "\nPress Enter to close this window.")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	os.Exit(1)
}

func newRootCmd() *cobra.Command {
	// By default Cobra refuses to run when started from Explorer, printing
	// "This is a command line tool" and exiting after 5 seconds. Envry is
	// meant to be double-clicked and started from the Start menu, so turn
	// that off; runServe handles those launches itself.
	cobra.MousetrapHelpText = ""

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
	err = serve(cmd, cfg)
	if err != nil && cfg.Managed {
		_ = writeStatus(cmd.OutOrStdout(), errorStatus(err))
	}
	return err
}

func serve(cmd *cobra.Command, cfg *config.Config) error {
	log := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: cfg.LogLevel}))
	log.Info("starting envry", "version", version.Version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.Managed {
		var cancel context.CancelFunc
		ctx, cancel = stopOnEOF(ctx, cmd.InOrStdin())
		defer cancel()
	}

	a, err := app.New(ctx, cfg, log)
	if err != nil {
		return err
	}
	if err := a.Listen(); err != nil {
		a.Store().Close()
		return err
	}
	switch {
	case cfg.Managed:
		if err := writeStatus(cmd.OutOrStdout(), readyStatus(a.Addrs())); err != nil {
			log.Warn("could not report status", "err", err)
		}
	case ownsConsole():
		// Started by double-click: say what is running and open the web UI,
		// since the user has no terminal command to go by.
		welcome(cmd.OutOrStdout(), a.Addrs())
		if err := openBrowser("http://" + localAddr(a.Addrs().HTTP)); err != nil {
			log.Warn("could not open the browser", "err", err)
		}
	}
	if err := a.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// welcome lists where everything is, for users who start envry.exe from
// Explorer or the Start menu rather than a terminal.
func welcome(w io.Writer, addrs app.Addrs) {
	fmt.Fprintf(w, "\nEnvry %s is running.\n\n", version.Version)
	rows := [][2]string{
		{"Web UI", "http://" + localAddr(addrs.HTTP)},
		{"SMTP", localAddr(addrs.SMTP) + "   send your app's mail here"},
		{"POP3", localAddr(addrs.POP3)},
		{"IMAP", localAddr(addrs.IMAP)},
	}
	for _, tls := range [][2]string{{"SMTPS", addrs.SMTPS}, {"POP3S", addrs.POP3S}, {"IMAPS", addrs.IMAPS}} {
		if tls[1] != "" {
			rows = append(rows, [2]string{tls[0], localAddr(tls[1])})
		}
	}
	for _, r := range rows {
		fmt.Fprintf(w, "  %-7s %s\n", r[0], r[1])
	}
	fmt.Fprint(w, "\nClose this window or press Ctrl+C to stop Envry.\n\n")
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
