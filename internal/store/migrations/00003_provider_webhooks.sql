-- +goose Up

-- Where Envry sends a provider's delivery events (delivered, bounced,
-- complained) for mail sent through that provider's emulated API, and the
-- secret it signs them with: a Resend (Svix) signing secret, a Mailgun
-- signing key or a SendGrid ECDSA private key, depending on the provider.
CREATE TABLE provider_webhooks (
  provider   TEXT PRIMARY KEY,
  url        TEXT NOT NULL DEFAULT '',
  secret     TEXT NOT NULL DEFAULT '',
  enabled    INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE provider_webhooks;
