package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestUsersSessionsAndSettings(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	n, err := st.CountUsers(ctx)
	if err != nil || n != 0 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	user, err := st.CreateAdmin(ctx, "admin", "hash", "admin@example.com", "en")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdmin(ctx, "second", "hash", "", "en"); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("expected setup done, got %v", err)
	}
	if _, err := st.GetUserByUsername(ctx, "ADMIN"); err != nil {
		t.Fatalf("case-insensitive lookup failed: %v", err)
	}
	if got, err := st.GetUserByEmailOrUsername(ctx, "admin@example.com"); err != nil || got.ID != user.ID {
		t.Fatalf("email lookup: %#v err=%v", got, err)
	}
	updated, err := st.UpdateUserProfile(ctx, user.ID, "new@example.com", "zh-CN", "dark")
	if err != nil || updated.Language != "zh-CN" || updated.Theme != "dark" {
		t.Fatalf("profile: %#v err=%v", updated, err)
	}
	pref, err := st.UpdateUserMediaLibrary(ctx, user.ID, "archive")
	if err != nil || pref.MediaLibrary != "archive" {
		t.Fatalf("media library preference: %#v err=%v", pref, err)
	}
	if err := st.UpdatePassword(ctx, user.ID, "newhash"); err != nil {
		t.Fatal(err)
	}

	token, csrf, expires, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if expires.Before(time.Now().Add(29 * 24 * time.Hour)) {
		t.Fatalf("session expires too soon: %v", expires)
	}
	sess, err := st.GetSession(ctx, token)
	if err != nil || sess.User.ID != user.ID || sess.CSRF != csrf || sess.User.PasswordHash != "newhash" {
		t.Fatalf("session: %#v err=%v", sess, err)
	}
	if _, err := st.GetSession(ctx, "invalid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if err := st.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session should be gone, got %v", err)
	}

	if err := st.SetSetting(ctx, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := st.GetSetting(ctx, "theme"); err != nil || !ok || v != "dark" {
		t.Fatalf("setting: %q %v %v", v, ok, err)
	}
	if err := st.SetSettings(ctx, map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatal(err)
	}
	all, err := st.Settings(ctx)
	if err != nil || all["a"] != "1" || all["b"] != "2" {
		t.Fatalf("settings: %#v err=%v", all, err)
	}
}

func TestPasswordResetSingleUse(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	user, err := st.CreateAdmin(ctx, "admin", "hash", "admin@example.com", "en")
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.CreatePasswordReset(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.ConsumePasswordReset(ctx, token)
	if err != nil || got != user.ID {
		t.Fatalf("consume: id=%d err=%v", got, err)
	}
	if _, err := st.ConsumePasswordReset(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token should be single-use, got %v", err)
	}
	token2, _ := st.CreatePasswordReset(ctx, user.ID)
	token3, _ := st.CreatePasswordReset(ctx, user.ID)
	if _, err := st.ConsumePasswordReset(ctx, token2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("older reset token should be invalidated, got %v", err)
	}
	if _, err := st.ConsumePasswordReset(ctx, token3); err != nil {
		t.Fatalf("newest reset token failed: %v", err)
	}
}

func TestMediaLibraryColumnMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")

	// Simulate a database created before the media_library preference existed.
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE COLLATE NOCASE,
		password_hash TEXT NOT NULL,
		email TEXT NOT NULL DEFAULT '',
		language TEXT NOT NULL DEFAULT 'en',
		theme TEXT NOT NULL DEFAULT 'system',
		role TEXT NOT NULL DEFAULT 'admin',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := db.ExecContext(ctx, `INSERT INTO users
		(username, password_hash, email, language, theme, role, created_at, updated_at)
		VALUES ('legacy', 'hash', '', 'en', 'system', 'admin', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	user, err := st.GetUserByUsername(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if user.MediaLibrary != "" {
		t.Fatalf("migrated preference = %q, want empty", user.MediaLibrary)
	}
	if _, err := st.UpdateUserMediaLibrary(ctx, user.ID, "archive"); err != nil {
		t.Fatal(err)
	}
	user, err = st.GetUserByUsername(ctx, "legacy")
	if err != nil || user.MediaLibrary != "archive" {
		t.Fatalf("preference after update: %#v err=%v", user, err)
	}
	if err := st.ClearUserMediaLibrary(ctx, "archive"); err != nil {
		t.Fatal(err)
	}
	user, err = st.GetUserByUsername(ctx, "legacy")
	if err != nil || user.MediaLibrary != "" {
		t.Fatalf("preference after clear: %#v err=%v", user, err)
	}
}
