package store

import (
	"context"
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
