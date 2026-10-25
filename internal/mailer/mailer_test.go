package mailer

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/settings"
)

func TestBuildMessage(t *testing.T) {
	from, err := mail.ParseAddress("panel@example.com")
	if err != nil {
		t.Fatal(err)
	}
	to, err := mail.ParseAddress("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	body, err := buildMessage(from, to, "Hugo build failed", "Plain body", "<p>HTML body</p>", "Hugo Panel")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "Hugo build failed" {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
	if msg.Header.Get("From") == "" || !strings.Contains(msg.Header.Get("From"), "Hugo Panel") {
		t.Fatalf("unexpected From header: %q", msg.Header.Get("From"))
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type: %s %#v err=%v", mediaType, params, err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	parts := 0
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		partText := new(strings.Builder)
		if _, err := io.Copy(partText, part); err != nil {
			t.Fatal(err)
		}
		if partText.Len() == 0 {
			t.Fatal("empty mime part")
		}
		parts++
	}
	if parts != 2 {
		t.Fatalf("expected 2 mime parts, got %d", parts)
	}
}

func TestPasswordResetEmail(t *testing.T) {
	msg := PasswordResetEmail("admin", "https://panel.example.com/#/reset?token=abc", time.Hour)
	if msg.Subject == "" || !strings.Contains(msg.Text, "https://panel.example.com/#/reset?token=abc") {
		t.Fatalf("unexpected message: %#v", msg)
	}
	if !strings.Contains(msg.HTML, "Choose a new password") || !strings.Contains(msg.HTML, "1 hour") {
		t.Fatalf("unexpected html: %s", msg.HTML)
	}
}

func TestSendWithoutConfig(t *testing.T) {
	m := New()
	err := m.Send(t.Context(), settings.SMTP{}, Message{To: "user@example.com", Subject: "x", Text: "x"})
	if err == nil {
		t.Fatal("expected an error for unconfigured SMTP")
	}
}
