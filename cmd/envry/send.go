package main

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/arifurrahmanns/envry/internal/compose"
	"github.com/arifurrahmanns/envry/internal/config"
)

type sendOptions struct {
	from, subject, text, html string
	to, cc, bcc, attach       []string
	user, password            string
}

func newSendCmd(flags *config.Flags) *cobra.Command {
	var o sendOptions
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a test email to a running Envry over SMTP",
		Long: `Send a test email to Envry's SMTP server (the --smtp address).

With --user, the message is sent with SMTP AUTH; when the user is an
account, a copy is filed in its Sent folder.`,
		Example: `  envry send --to dev@test.local --subject "Hello"
  envry send --to dev@test.local --bcc audit@test.local --html "<h1>Hi</h1>" --attach report.pdf`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := flags.Load(cmd.Flags(), os.LookupEnv)
			if err != nil {
				return err
			}
			return runSend(cmd, dialAddr(cfg.SMTPAddr), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.from, "from", "envry@localhost", "sender address")
	f.StringArrayVar(&o.to, "to", nil, "recipient, repeatable")
	f.StringArrayVar(&o.cc, "cc", nil, "Cc recipient, repeatable")
	f.StringArrayVar(&o.bcc, "bcc", nil, "Bcc recipient, repeatable (not written to the headers)")
	f.StringVar(&o.subject, "subject", "Test email from envry send", "subject line")
	f.StringVar(&o.text, "text", "", "plain-text body (default: a short test message)")
	f.StringVar(&o.html, "html", "", "HTML body")
	f.StringArrayVar(&o.attach, "attach", nil, "file to attach, repeatable")
	f.StringVar(&o.user, "user", "", "SMTP AUTH username")
	f.StringVar(&o.password, "password", "", "SMTP AUTH password")
	return cmd
}

func runSend(cmd *cobra.Command, addr string, o sendOptions) error {
	from, err := mail.ParseAddress(o.from)
	if err != nil {
		return fmt.Errorf("--from: invalid address %q", o.from)
	}
	msg := compose.Message{From: from, Subject: o.subject, Text: o.text, HTML: o.html}
	for _, list := range []struct {
		flag string
		in   []string
		out  *[]*mail.Address
	}{{"--to", o.to, &msg.To}, {"--cc", o.cc, &msg.Cc}, {"--bcc", o.bcc, &msg.Bcc}} {
		if *list.out, err = compose.ParseAddresses(list.in); err != nil {
			return fmt.Errorf("%s: %w", list.flag, err)
		}
	}
	if len(msg.Recipients()) == 0 {
		return errors.New("at least one of --to, --cc or --bcc is required")
	}
	if msg.Text == "" && msg.HTML == "" {
		msg.Text = "This is a test email sent with envry send."
	}
	for _, path := range o.attach {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("--attach: %w", err)
		}
		msg.Attachments = append(msg.Attachments, compose.Attachment{Filename: filepath.Base(path), Data: data})
	}

	raw, err := compose.Build(msg)
	if err != nil {
		return err
	}
	if err := deliver(addr, from.Address, msg.Recipients(), raw, o.user, o.password); err != nil {
		return fmt.Errorf("send to %s: %w", addr, err)
	}
	n := len(msg.Recipients())
	fmt.Fprintf(cmd.OutOrStdout(), "Sent %q to %d recipient%s via %s\n", msg.Subject, n, plural(n), addr)
	return nil
}

func deliver(addr, from string, rcpts []string, raw []byte, user, password string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if user != "" {
		host, _, _ := net.SplitHostPort(addr)
		if err := c.Auth(smtp.PlainAuth("", user, password, host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("recipient %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// dialAddr turns a wildcard listen address into one a client can dial.
func dialAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
