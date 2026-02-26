package internal

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Generates a cryptographically secure random base64 string of the given length
func GenerateAPIKeyBase64(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes), nil
}

// Creates a new membership key for the given channel, limiting the number of keys to two.
// expiration is set by the current time + ttl.
// This is not a hard limit, and will change based on the current ttl config.
func (a *App) CreateMembershipKey(ctx context.Context, channel string) (newKey string, expiration time.Time, err error) {
	// 1. Generate new Key
	newKey, err = GenerateAPIKeyBase64(32)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate API key: %w", err)
	}
	createdAt := time.Now()

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 2. Count existing keys for channel
	rows, err := tx.QueryContext(ctx, "SELECT key, created_at FROM membership_keys WHERE channel = ? ORDER BY created_at ASC", channel)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to query existing keys: %w", err)
	}
	defer rows.Close()

	var keys []struct {
		Key       string
		CreatedAt string
	}
	for rows.Next() {
		var k, c string
		if err := rows.Scan(&k, &c); err != nil {
			return "", time.Time{}, fmt.Errorf("failed to scan key: %w", err)
		}
		keys = append(keys, struct {
			Key       string
			CreatedAt string
		}{k, c})
	}

	// 3. Prune if needed (Limit 2 active)
	if len(keys) >= 2 {
		args := make([]any, len(keys)-1)
		var placeholders strings.Builder
		for i := range args {
			args[i] = keys[i].Key
			if i > 0 {
				placeholders.WriteString(",")
			}
			placeholders.WriteString("?")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM membership_keys WHERE key IN ("+placeholders.String()+")", args...); err != nil {
			return "", time.Time{}, fmt.Errorf("failed to delete old keys: %w", err)
		}
	}

	// 4. Insert new key
	_, err = tx.ExecContext(ctx, "INSERT INTO membership_keys (key, channel, created_at) VALUES (?, ?, ?)", newKey, channel, createdAt.Format(time.RFC3339))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to insert new key: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Calculate expiration based on *current* config TTL
	expiration = createdAt.Add(time.Duration(a.config.KeyTTLDays) * 24 * time.Hour)
	return newKey, expiration, nil
}

// Retrieves all keys for a channel and their expiration.
//
//	keys = GetMembershipKeys("channel")
//	keys["key"] -> expiration
func (a *App) GetMembershipKeys(ctx context.Context, channel string) (keyToExpiration map[string]time.Time, err error) {
	rows, err := a.db.QueryContext(ctx, "SELECT key, created_at FROM membership_keys WHERE channel = ?", channel)
	if err != nil {
		return nil, fmt.Errorf("failed to query keys: %w", err)
	}
	defer rows.Close()

	results := make(map[string]time.Time)
	for rows.Next() {
		var key, createdStr string
		if err := rows.Scan(&key, &createdStr); err != nil {
			return nil, fmt.Errorf("failed to scan key: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339, createdStr)
		if err != nil {
			slog.Error("failed to parse created_at", "err", err)
			continue
		}
		// Expiration is dynamic based on current config
		expiration := createdAt.Add(time.Duration(a.config.KeyTTLDays) * 24 * time.Hour)
		results[key] = expiration
	}
	return results, nil
}

// Retrieves all keys for all channels and their expiration.
//
//	keys = GetAllMembershipKeys()
//	keys["channel"]["key"] -> expiration
func (a *App) GetAllMembershipKeys(ctx context.Context) (channelAndKeyToExpiration map[string]map[string]time.Time, err error) {
	rows, err := a.db.QueryContext(ctx, "SELECT channel, key, created_at FROM membership_keys")
	if err != nil {
		return nil, fmt.Errorf("failed to query all keys: %w", err)
	}
	defer rows.Close()

	results := make(map[string]map[string]time.Time)
	for rows.Next() {
		var channel, key, createdStr string
		if err := rows.Scan(&channel, &key, &createdStr); err != nil {
			return nil, fmt.Errorf("failed to scan key: %w", err)
		}
		createdAt, err := time.Parse(time.RFC3339, createdStr)
		if err != nil {
			slog.Error("failed to parse created_at", "err", err)
			continue
		}
		expiration := createdAt.Add(time.Duration(a.config.KeyTTLDays) * 24 * time.Hour)

		if _, ok := results[channel]; !ok {
			results[channel] = make(map[string]time.Time)
		}
		results[channel][key] = expiration
	}
	return results, nil
}

// Deletes all keys for the given channel
func (a *App) DeleteMembershipKeys(ctx context.Context, channel string) error {
	_, err := a.db.ExecContext(ctx, "DELETE FROM membership_keys WHERE channel = ?", channel)
	if err != nil {
		return fmt.Errorf("failed to delete keys: %w", err)
	}
	return nil
}

// Verifies a membership key and returns the channel name that it belongs to. Returns empty string if invalid.
// Also lazily deletes expired keys if found.
func (a *App) VerifyMembershipKey(ctx context.Context, key string) (channel string, err error) {
	if key == "" {
		return "", nil
	}

	var createdStr string
	err = a.db.QueryRowContext(ctx, "SELECT channel, created_at FROM membership_keys WHERE key = ?", key).Scan(&channel, &createdStr)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to query key: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse created_at: %w", err)
	}

	// Check Expiry
	expiration := createdAt.Add(time.Duration(a.config.KeyTTLDays) * 24 * time.Hour)
	if time.Now().After(expiration) {
		_, _ = a.db.ExecContext(ctx, "DELETE FROM membership_keys WHERE key = ?", key)
		return "", nil
	}

	return channel, nil
}

// Checks if configured channels have a key, and generates one if not.
func (a *App) EnsureMembershipKeys(ctx context.Context) error {
	for _, channel := range a.config.Membership {
		keys, err := a.GetMembershipKeys(ctx, channel)
		if err != nil {
			slog.Error("failed to check membership keys on startup", "channel", channel, "err", err)
			continue // Non-fatal, try next
		}

		if len(keys) == 0 {
			_, _, err := a.CreateMembershipKey(ctx, channel)
			if err != nil {
				slog.Error("failed to create initial membership key", "channel", channel, "err", err)
				continue
			}
			slog.Info("Generated initial membership key", "channel", channel)
		}
	}
	return nil
}
