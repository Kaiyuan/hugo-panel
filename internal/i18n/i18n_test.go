package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Kaiyuan/hugo-panel/web"
)

var placeholderRe = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	locales, err := fs.Sub(web.Files, "locales")
	if err != nil {
		t.Fatal(err)
	}
	return New(locales, t.TempDir())
}

func TestBuiltinLanguagesAreComplete(t *testing.T) {
	m := newTestManager(t)
	languages, err := m.Languages()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"en", "zh-CN", "zh-TW", "fr", "it", "es", "ja", "ko", "de"}
	if len(languages) != len(want) {
		t.Fatalf("got %d languages: %#v", len(languages), languages)
	}
	for i, code := range want {
		if languages[i].Code != code || !languages[i].Builtin {
			t.Fatalf("language %d = %#v, want %s", i, languages[i], code)
		}
	}
	en, err := m.Messages("en")
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range languages {
		messages, err := m.Messages(lang.Code)
		if err != nil {
			t.Fatalf("%s: %v", lang.Code, err)
		}
		for key, source := range en {
			if key == "" || key[0] == '_' {
				continue
			}
			got, ok := messages[key]
			if !ok || got == "" {
				t.Errorf("%s missing key %s", lang.Code, key)
				continue
			}
			wantPlaceholders := placeholderRe.FindAllString(source, -1)
			gotPlaceholders := placeholderRe.FindAllString(got, -1)
			if len(wantPlaceholders) != len(gotPlaceholders) {
				t.Errorf("%s key %s placeholder count mismatch: %q vs %q", lang.Code, key, wantPlaceholders, gotPlaceholders)
				continue
			}
			wantSet := map[string]bool{}
			for _, p := range wantPlaceholders {
				wantSet[p] = true
			}
			for _, p := range gotPlaceholders {
				if !wantSet[p] {
					t.Errorf("%s key %s placeholder mismatch: %q vs %q", lang.Code, key, wantPlaceholders, gotPlaceholders)
					break
				}
			}
		}
	}
}

func TestHugoBrandNameIsNotLocalized(t *testing.T) {
	m := newTestManager(t)
	languages, err := m.Languages()
	if err != nil {
		t.Fatal(err)
	}
	localizedForms := []string{"雨果", "ヒューゴ", "휴고", "Ugo"}
	brandKeys := []string{
		"content.tags_hint",
		"dashboard.default_site",
		"dashboard.hugo_version",
		"plugins.hugo_module",
		"settings.hugo",
		"settings.hugo_version",
	}
	for _, lang := range languages {
		messages, err := m.Messages(lang.Code)
		if err != nil {
			t.Fatalf("%s: %v", lang.Code, err)
		}
		for key, value := range messages {
			for _, form := range localizedForms {
				if strings.Contains(value, form) {
					t.Errorf("%s key %s translates Hugo as %q: %q", lang.Code, key, form, value)
				}
			}
		}
		for _, key := range brandKeys {
			if !strings.Contains(messages[key], "Hugo") {
				t.Errorf("%s key %s must keep the brand name Hugo: %q", lang.Code, key, messages[key])
			}
		}
		for _, key := range []string{"dashboard.hugo_version", "settings.hugo_version"} {
			if messages[key] != "Hugo" {
				t.Errorf("%s key %s must be exactly %q: %q", lang.Code, key, "Hugo", messages[key])
			}
		}
	}
}

func TestCustomTranslationLifecycle(t *testing.T) {
	m := newTestManager(t)
	err := m.AddCustom("nl", "Nederlands", map[string]string{
		"nav.dashboard": "Overzicht",
		"common.cancel": "Annuleren",
	})
	if err != nil {
		t.Fatal(err)
	}
	languages, err := m.Languages()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, lang := range languages {
		if lang.Code == "nl" {
			found = true
			if lang.Builtin || lang.NativeName != "Nederlands" {
				t.Fatalf("custom language: %#v", lang)
			}
		}
	}
	if !found {
		t.Fatal("custom language not listed")
	}
	messages, err := m.Messages("nl")
	if err != nil {
		t.Fatal(err)
	}
	if messages["nav.dashboard"] != "Overzicht" || messages["common.cancel"] != "Annuleren" {
		t.Fatalf("custom messages: %#v", messages)
	}
	if messages["nav.content"] == "" {
		t.Fatal("missing keys should fall back to English")
	}
	if !m.IsBuiltin("nl") == false {
		t.Fatal("nl should not be builtin")
	}
	if err := m.DeleteCustom("nl"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Messages("nl"); err != nil {
		t.Fatalf("fallback after deleting custom locale failed: %v", err)
	}
}

func TestInvalidCustomTranslation(t *testing.T) {
	m := newTestManager(t)
	if err := m.AddCustom("../evil", "Evil", map[string]string{"a": "b"}); err == nil {
		t.Fatal("expected invalid code error")
	}
	if err := m.AddCustom("nl", "", map[string]string{"a": "b"}); err == nil {
		t.Fatal("expected invalid name error")
	}
	if err := m.AddCustom("nl", "Nederlands", map[string]string{}); err == nil {
		t.Fatal("expected empty translation error")
	}
}

func TestCustomLocaleWithUnderscoreFile(t *testing.T) {
	m := newTestManager(t)
	if err := os.MkdirAll(m.customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"_name":"Portuguese (Brazil)","_native_name":"Português (Brasil)","nav.dashboard":"Painel"}`
	if err := os.WriteFile(filepath.Join(m.customDir, "pt_BR.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	languages, err := m.Languages()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, lang := range languages {
		if lang.Code == "pt-BR" {
			found = true
			if lang.NativeName != "Português (Brasil)" {
				t.Fatalf("unexpected metadata: %#v", lang)
			}
		}
	}
	if !found {
		t.Fatalf("pt_BR locale not listed as pt-BR: %#v", languages)
	}
	messages, err := m.Messages("pt_BR")
	if err != nil || messages["nav.dashboard"] != "Painel" {
		t.Fatalf("underscore locale lookup failed: %#v err=%v", messages["nav.dashboard"], err)
	}
	if err := m.DeleteCustom("pt-BR"); err != nil {
		t.Fatalf("delete underscore locale: %v", err)
	}
}
