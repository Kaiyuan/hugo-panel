package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/settings"
)

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Mailer struct{}

func New() *Mailer { return &Mailer{} }

func (m *Mailer) Send(ctx context.Context, cfg settings.SMTP, msg Message) error {
	if !cfg.Configured() {
		return fmt.Errorf("smtp is not configured")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("invalid SMTP_FROM address: %w", err)
	}
	to, err := mail.ParseAddress(strings.TrimSpace(msg.To))
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	subject := strings.ReplaceAll(strings.ReplaceAll(msg.Subject, "\r", " "), "\n", " ")
	host, err := smtpHost(cfg.Host)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(cfg.Port))
	dialer := net.Dialer{Timeout: 20 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))

	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if cfg.Security == "tls" {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("SMTP TLS handshake: %w", err)
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("SMTP client: %w", err)
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("SMTP hello: %w", err)
	}
	if cfg.Security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}
	if cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", cfg.Username, cfg.Password, host)
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("SMTP authentication: %w", err)
			}
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("SMTP RCPT TO: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	body, err := buildMessage(from, to, subject, msg.Text, msg.HTML, cfg.FromName)
	if err != nil {
		return err
	}
	if _, err := wc.Write(body); err != nil {
		return fmt.Errorf("write SMTP body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("finish SMTP body: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("SMTP quit: %w", err)
	}
	return nil
}

func smtpHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("SMTP host is empty")
	}
	if strings.ContainsAny(host, " \r\n\t/\\") {
		return "", fmt.Errorf("invalid SMTP host")
	}
	return host, nil
}

func buildMessage(from, to *mail.Address, subject, text, htmlBody, fromName string) ([]byte, error) {
	if text == "" {
		text = stripHTML(htmlBody)
	}
	var buf bytes.Buffer
	writeHeader := func(k, v string) {
		buf.WriteString(k)
		buf.WriteString(": ")
		buf.WriteString(v)
		buf.WriteString("\r\n")
	}
	fromHeader := from.String()
	if fromName != "" {
		fromHeader = (&mail.Address{Name: fromName, Address: from.Address}).String()
	}
	writeHeader("From", fromHeader)
	writeHeader("To", to.String())
	writeHeader("Subject", mime.QEncoding.Encode("utf-8", subject))
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	writeHeader("MIME-Version", "1.0")
	writeHeader("Message-ID", messageID(from.Address))
	writeHeader("Auto-Submitted", "auto-generated")

	mw := multipart.NewWriter(&buf)
	writeHeader("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	buf.WriteString("\r\n")

	if err := writePart(mw, "text/plain; charset=utf-8", text); err != nil {
		return nil, err
	}
	if htmlBody != "" {
		if err := writePart(mw, "text/html; charset=utf-8", htmlBody); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writePart(mw *multipart.Writer, contentType, body string) error {
	h := make(map[string][]string)
	h["Content-Type"] = []string{contentType}
	h["Content-Transfer-Encoding"] = []string{"quoted-printable"}
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}
	return qp.Close()
}

func messageID(from string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	domain := "hugo-panel.local"
	if at := strings.LastIndex(from, "@"); at >= 0 && at < len(from)-1 {
		domain = from[at+1:]
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), hex.EncodeToString(b), domain)
}

func stripHTML(s string) string {
	// Tiny fallback: the caller always provides plain text for important mail.
	if s == "" {
		return ""
	}
	return html.UnescapeString(s)
}

func PasswordResetEmail(displayName, link string, expires time.Duration) Message {
	name := displayName
	if name == "" {
		name = "there"
	}
	subject := "Reset your Hugo Panel password"
	text := fmt.Sprintf(`Hello %s,

A password reset was requested for your Hugo Panel account.

Open this link to choose a new password (valid for %s):
%s

If you did not request this, you can safely ignore this email.
`, name, humanDuration(expires), link)
	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="margin:0;padding:32px;background:#f6f7f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0"><tr><td align="center">
<table role="presentation" width="520" cellpadding="0" cellspacing="0" style="background:#fff;border:1px solid #e7e9ee;border-radius:16px;padding:36px">
<tr><td>
<div style="font-size:14px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:#6b7280">Hugo Panel</div>
<h1 style="margin:14px 0 10px;font-size:26px;line-height:1.25">Reset your password</h1>
<p style="margin:0 0 22px;font-size:15px;line-height:1.6;color:#4b5563">Hello %s, a password reset was requested for your account. The link below is valid for %s.</p>
<p style="margin:0 0 26px"><a href="%s" style="display:inline-block;background:#111;color:#fff;text-decoration:none;font-weight:600;padding:12px 20px;border-radius:10px;font-size:15px">Choose a new password</a></p>
<p style="margin:0;font-size:14px;line-height:1.6;color:#6b7280">If you did not request this, you can ignore this email. If the button does not work, copy this link:<br><span style="word-break:break-all;color:#111">%s</span></p>
</td></tr></table></td></tr></table></body></html>`, html.EscapeString(name), humanDuration(expires), html.EscapeString(link), html.EscapeString(link))
	return Message{Subject: subject, Text: text, HTML: htmlBody}
}

func humanDuration(d time.Duration) string {
	if d >= time.Hour {
		h := int(d / time.Hour)
		if h == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", h)
	}
	return d.String()
}

// EncodeBase64 is exposed for tests and future attachments support.
func EncodeBase64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
