package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ProviderWebhook is where Envry sends one provider's delivery events.
type ProviderWebhook struct {
	Provider  string    `json:"provider"`
	URL       string    `json:"url"`
	Secret    string    `json:"-"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProviderWebhook returns a provider's webhook settings; found is false
// when none were saved.
func (s *Store) ProviderWebhook(ctx context.Context, provider string) (w ProviderWebhook, found bool, err error) {
	var updated int64
	err = s.db.QueryRowContext(ctx,
		`SELECT provider, url, secret, enabled, updated_at FROM provider_webhooks WHERE provider = ?`, provider).
		Scan(&w.Provider, &w.URL, &w.Secret, &w.Enabled, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderWebhook{Provider: provider}, false, nil
	}
	if err != nil {
		return ProviderWebhook{}, false, fmt.Errorf("read %s webhook: %w", provider, err)
	}
	w.UpdatedAt = fromMillis(updated)
	return w, true, nil
}

// SaveProviderWebhook creates or replaces a provider's webhook settings.
func (s *Store) SaveProviderWebhook(ctx context.Context, w ProviderWebhook) (ProviderWebhook, error) {
	w.UpdatedAt = s.now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_webhooks (provider, url, secret, enabled, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (provider) DO UPDATE SET
			url = excluded.url, secret = excluded.secret, enabled = excluded.enabled, updated_at = excluded.updated_at`,
		w.Provider, w.URL, w.Secret, w.Enabled, millis(w.UpdatedAt))
	if err != nil {
		return ProviderWebhook{}, fmt.Errorf("save %s webhook: %w", w.Provider, err)
	}
	return w, nil
}
