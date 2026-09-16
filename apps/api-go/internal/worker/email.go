package worker

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
	"regexp"
	"strconv"
	"strings"
)

//go:embed templates/emails
var emailTemplates embed.FS

// EmailSettings mirrors get_email_configuration, which reads the instance
// configuration rows and falls back to the environment.
type EmailSettings struct {
	Host     string
	User     string
	Password string
	Port     string
	UseTLS   string
	UseSSL   string
	From     string
}

// ConfigurationReader resolves an instance configuration value, decrypting it
// the way Django does when the value is stored encrypted.
type ConfigurationReader interface {
	ConfigurationValue(ctx context.Context, key, fallback string) (string, error)
}

// Mailer sends one message. It is an interface so tests can capture instead of
// dialing a server.
type Mailer interface {
	Send(ctx context.Context, settings EmailSettings, to, subject, text, html string) error
	// SendAttachment sends a message whose body is plain text and which carries one file. It is a second method rather than an option on the first because Django builds the two differently: the notification emails are multipart/alternative with an html part, and the export emails are multipart/mixed with no html part at all.
	SendAttachment(ctx context.Context, settings EmailSettings, to, subject, text, filename, contentType string, content []byte) error
}

type SMTPMailer struct{}

// Send reproduces Django's get_connection plus EmailMultiAlternatives: a plain
// text body with an HTML alternative, over TLS, SSL, or neither.
func (mailer SMTPMailer) Send(ctx context.Context, settings EmailSettings, to, subject, text, html string) error {
	return mailer.deliver(ctx, settings, to, buildMultipartMessage(settings.From, to, subject, text, html))
}

// SendAttachment reproduces EmailMultiAlternatives with one attached file and no html alternative, which is what the export emails are.
func (mailer SMTPMailer) SendAttachment(ctx context.Context, settings EmailSettings, to, subject, text, filename, contentType string, content []byte) error {
	return mailer.deliver(ctx, settings, to, buildAttachmentMessage(settings.From, to, subject, text, filename, contentType, content))
}

func (SMTPMailer) deliver(ctx context.Context, settings EmailSettings, to, message string) error {
	port := settings.Port
	if port == "" {
		port = "587"
	}
	address := net.JoinHostPort(settings.Host, port)

	dialer := &net.Dialer{}
	var connection net.Conn
	var err error
	if settings.UseSSL == "1" {
		connection, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: settings.Host})
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("dial smtp %s: %w", address, err)
	}
	client, err := smtp.NewClient(connection, settings.Host)
	if err != nil {
		connection.Close()
		return fmt.Errorf("start smtp session: %w", err)
	}
	defer client.Close()

	if settings.UseTLS == "1" && settings.UseSSL != "1" {
		if err := client.StartTLS(&tls.Config{ServerName: settings.Host}); err != nil {
			return fmt.Errorf("start tls: %w", err)
		}
	}
	if settings.User != "" {
		auth := smtp.PlainAuth("", settings.User, settings.Password, settings.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(envelopeAddress(settings.From)); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		writer.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close message: %w", err)
	}
	return client.Quit()
}

// envelopeAddress strips the display name from a "Name <addr>" from header.
func envelopeAddress(from string) string {
	start := strings.LastIndex(from, "<")
	end := strings.LastIndex(from, ">")
	if start >= 0 && end > start {
		return from[start+1 : end]
	}
	return strings.TrimSpace(from)
}

func buildMultipartMessage(from, to, subject, text, html string) string {
	boundary := "pace-go-boundary-0f2a1c"
	var builder strings.Builder
	builder.WriteString("From: " + from + "\r\n")
	builder.WriteString("To: " + to + "\r\n")
	builder.WriteString("Subject: " + encodeHeader(subject) + "\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	builder.WriteString("--" + boundary + "\r\n")
	builder.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	builder.WriteString(text + "\r\n")
	builder.WriteString("--" + boundary + "\r\n")
	builder.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
	builder.WriteString(html + "\r\n")
	builder.WriteString("--" + boundary + "--\r\n")
	return builder.String()
}

// buildAttachmentMessage is a plain text body with one file beside it, which is what Django's attach() produces when nothing was attached as an alternative.
func buildAttachmentMessage(from, to, subject, text, filename, contentType string, content []byte) string {
	boundary := "pace-go-mixed-0f2a1c"
	var builder strings.Builder
	builder.WriteString("From: " + from + "\r\n")
	builder.WriteString("To: " + to + "\r\n")
	builder.WriteString("Subject: " + encodeHeader(subject) + "\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n\r\n")
	builder.WriteString("--" + boundary + "\r\n")
	builder.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	builder.WriteString(text + "\r\n")
	builder.WriteString("--" + boundary + "\r\n")
	builder.WriteString("Content-Type: " + contentType + "; charset=\"utf-8\"\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Transfer-Encoding: base64\r\n")
	builder.WriteString("Content-Disposition: attachment; filename=\"" + filename + "\"\r\n\r\n")
	builder.WriteString(wrapBase64(content) + "\r\n")
	builder.WriteString("--" + boundary + "--\r\n")
	return builder.String()
}

// wrapBase64 breaks the encoded attachment at the line length a mail transport expects.
func wrapBase64(content []byte) string {
	encoded := base64.StdEncoding.EncodeToString(content)
	var builder strings.Builder
	for start := 0; start < len(encoded); start += 76 {
		end := start + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		if start > 0 {
			builder.WriteString("\r\n")
		}
		builder.WriteString(encoded[start:end])
	}
	return builder.String()
}

// encodeHeader applies RFC 2047 encoding when the subject is not plain ASCII,
// which is what Django's message builder does.
func encodeHeader(value string) string {
	for _, character := range value {
		if character > 127 {
			return mimeEncode(value)
		}
	}
	return value
}

func mimeEncode(value string) string {
	return "=?utf-8?b?" + base64Encode(value) + "?="
}

// renderEmail renders one of the embedded templates with the Django context.
func renderEmail(name string, context map[string]any) (string, error) {
	parsed, err := template.ParseFS(emailTemplates, "templates/"+name)
	if err != nil {
		return "", fmt.Errorf("parse email template %s: %w", name, err)
	}
	var builder strings.Builder
	if err := parsed.Execute(&builder, context); err != nil {
		return "", fmt.Errorf("render email template %s: %w", name, err)
	}
	return builder.String(), nil
}

var stylePattern = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
var tagPattern = regexp.MustCompile(`(?s)<[^>]*>`)
var blankLinePattern = regexp.MustCompile(`\n\s*\n\s*\n+`)

// plainTextFromHTML reproduces generate_plain_text_from_html: drop style
// blocks, strip tags, collapse runs of blank lines, and pad with one blank line
// on each side. Django's strip_tags leaves entities alone, so &amp; stays
// escaped in the plain text part; that is reproduced rather than unescaped.
func plainTextFromHTML(html string) string {
	stripped := stylePattern.ReplaceAllString(html, "")
	stripped = tagPattern.ReplaceAllString(stripped, "")
	stripped = blankLinePattern.ReplaceAllString(stripped, "\n\n")
	return "\n\n" + strings.TrimSpace(stripped) + "\n\n"
}

// emailSettings reads the same seven keys get_email_configuration reads.
func emailSettings(ctx context.Context, reader ConfigurationReader, defaults EmailSettings) (EmailSettings, error) {
	if reader == nil {
		return defaults, nil
	}
	resolved := defaults
	for _, field := range []struct {
		key   string
		value *string
	}{
		{key: "EMAIL_HOST", value: &resolved.Host},
		{key: "EMAIL_HOST_USER", value: &resolved.User},
		{key: "EMAIL_HOST_PASSWORD", value: &resolved.Password},
		{key: "EMAIL_PORT", value: &resolved.Port},
		{key: "EMAIL_USE_TLS", value: &resolved.UseTLS},
		{key: "EMAIL_USE_SSL", value: &resolved.UseSSL},
		{key: "EMAIL_FROM", value: &resolved.From},
	} {
		value, err := reader.ConfigurationValue(ctx, field.key, *field.value)
		if err != nil {
			return EmailSettings{}, err
		}
		*field.value = value
	}
	if _, err := strconv.Atoi(resolved.Port); err != nil && resolved.Port != "" {
		return EmailSettings{}, fmt.Errorf("EMAIL_PORT %q is not a number", resolved.Port)
	}
	return resolved, nil
}

// SendPlainEmail sends one message with no html part at all, which is what the admin console's credential check sends.
func SendPlainEmail(ctx context.Context, defaults EmailSettings, reader ConfigurationReader, mailer Mailer, to, subject, text string) error {
	settings, err := emailSettings(ctx, reader, defaults)
	if err != nil {
		return err
	}
	return mailer.Send(ctx, settings, to, subject, text, "")
}

// TestEmailSubject is what manage.py test_email sends under.
const TestEmailSubject = "Test email from Plane"

// SendTestEmail is manage.py test_email: one message to prove the mail settings work.
//
// Unlike the export emails this one really does carry an html alternative, and its plain text part is strip_tags rather than the fuller conversion the notification emails use — so the blank lines the template holds survive into it.
func SendTestEmail(ctx context.Context, defaults EmailSettings, reader ConfigurationReader, mailer Mailer, to string) error {
	html, err := renderEmail("emails/test_email.html", map[string]any{})
	if err != nil {
		return err
	}
	settings, err := emailSettings(ctx, reader, defaults)
	if err != nil {
		return err
	}
	return mailer.Send(ctx, settings, to, TestEmailSubject, stripTags(html), html)
}

// stripTags is django.utils.html.strip_tags, which drops the tags and leaves everything between them — entities included.
func stripTags(value string) string {
	return tagPattern.ReplaceAllString(value, "")
}
