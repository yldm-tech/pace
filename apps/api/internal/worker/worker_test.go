package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yldm-tech/pace/apps/api/internal/httpsafe"
)

// The expectations come from running generate_plain_text_from_html, which is
// Django's strip_tags plus the whitespace collapsing, over the same input.
func TestPlainTextMatchesDjango(t *testing.T) {
	for _, test := range []struct {
		in   string
		want string
	}{
		{in: "<style>body{color:red}</style><p>Hello <b>World</b></p>", want: "\n\nHello World\n\n"},
		{in: "<div>a</div>\n\n\n\n<div>b</div>", want: "\n\na\n\nb\n\n"},
		// strip_tags leaves entities escaped, so the plain text keeps &amp;.
		{in: "<p>Tom &amp; Jerry</p>", want: "\n\nTom &amp; Jerry\n\n"},
		{in: "<p>5 &lt; 6</p>", want: "\n\n5 &lt; 6\n\n"},
		{in: "  <p>  spaced  </p>  ", want: "\n\nspaced\n\n"},
		{in: "<p>line1<br/>line2</p>", want: "\n\nline1line2\n\n"},
		{in: "<STYLE>x</STYLE><p>after</p>", want: "\n\nafter\n\n"},
		{in: "<p>&nbsp;nbsp</p>", want: "\n\n&nbsp;nbsp\n\n"},
	} {
		if got := plainTextFromHTML(test.in); got != test.want {
			t.Errorf("plainTextFromHTML(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// Every task name points at a module the Django app really has. Most live under plane.bgtasks; the telemetry one lives beside the licence models instead.
func TestTaskNamesMatchDjangoModulePaths(t *testing.T) {
	for _, name := range MigratedTaskNames() {
		if !strings.HasPrefix(name, "plane.bgtasks.") && !strings.HasPrefix(name, "plane.license.bgtasks.") {
			t.Errorf("task name %q does not point at a Django task module", name)
		}
	}
}

func TestDecodeBodyReadsProtocolV2(t *testing.T) {
	body := []byte(`[["a@pace.test","key","123456"],{},{"callbacks":null}]`)
	arguments, keywords, err := decodeBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(arguments) != 3 || arguments[0] != "a@pace.test" || len(keywords) != 0 {
		t.Fatalf("arguments = %#v, keywords = %#v", arguments, keywords)
	}

	body = []byte(`[[],{"email":"b@pace.test","token":"999999"},{}]`)
	arguments, keywords, err = decodeBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(arguments) != 0 || keywords["email"] != "b@pace.test" {
		t.Fatalf("arguments = %#v, keywords = %#v", arguments, keywords)
	}
	if _, _, err := decodeBody([]byte(`{"not":"an envelope"}`)); err == nil {
		t.Fatal("a non-envelope body should fail to decode")
	}
}

// Django calls some tasks positionally and others with keywords, so a handler
// has to read either shape.
func TestArgumentsFallBackToKeywords(t *testing.T) {
	arguments := []any{"positional"}
	keywords := map[string]any{"second": "from-keywords"}
	if got := stringArgument(arguments, keywords, 0, "first"); got != "positional" {
		t.Fatalf("positional read = %q", got)
	}
	if got := stringArgument(arguments, keywords, 1, "second"); got != "from-keywords" {
		t.Fatalf("keyword read = %q", got)
	}
	if got := stringArgument(arguments, keywords, 5, "missing"); got != "" {
		t.Fatalf("missing read = %q", got)
	}
}

func TestEmailTasksRenderTheDjangoTemplates(t *testing.T) {
	mailer := &recordingMailer{}
	tasks := NewEmailTasks(nil, EmailSettings{From: "Team Plane <team@pace.test>"}, nil, mailer, nil)
	err := tasks.magicLink(context.Background(), []any{"user@pace.test", "magic_user@pace.test", "424242"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mailer.to != "user@pace.test" {
		t.Fatalf("recipient = %q", mailer.to)
	}
	if mailer.subject != "Your unique Plane login code is 424242" {
		t.Fatalf("subject = %q", mailer.subject)
	}
	if !strings.Contains(mailer.html, "424242") {
		t.Fatalf("rendered html is missing the code: %s", mailer.html)
	}
	if !strings.Contains(mailer.text, "424242") {
		t.Fatalf("plain text is missing the code: %s", mailer.text)
	}
	if strings.Contains(mailer.text, "<") {
		t.Fatalf("plain text still carries markup: %s", mailer.text)
	}
}

func TestForgotPasswordBuildsTheDjangoURL(t *testing.T) {
	mailer := &recordingMailer{}
	tasks := NewEmailTasks(nil, EmailSettings{}, nil, mailer, nil)
	err := tasks.forgotPassword(context.Background(),
		[]any{"Ada", "ada@pace.test", "uid64", "tok", "https://app.pace.test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://app.pace.test/accounts/reset-password/?uidb64=uid64&amp;token=tok&amp;email=ada@pace.test"
	if !strings.Contains(mailer.html, want) {
		t.Fatalf("rendered html is missing the reset url: %s", mailer.html)
	}
	if mailer.subject != "A new password to your Plane account has been requested" {
		t.Fatalf("subject = %q", mailer.subject)
	}
}

func TestEmailUpdateCodeReusesTheMagicTemplate(t *testing.T) {
	mailer := &recordingMailer{}
	tasks := NewEmailTasks(nil, EmailSettings{}, nil, mailer, nil)
	if err := tasks.emailUpdateCode(context.Background(), []any{"new@pace.test", "121212"}, nil); err != nil {
		t.Fatal(err)
	}
	if mailer.subject != "Verify your new email address" {
		t.Fatalf("subject = %q", mailer.subject)
	}
	if !strings.Contains(mailer.html, "121212") {
		t.Fatalf("rendered html is missing the code: %s", mailer.html)
	}
}

func TestEmailSettingsReadTheInstanceConfiguration(t *testing.T) {
	reader := fakeConfiguration{values: map[string]string{
		"EMAIL_HOST": "smtp.pace.test", "EMAIL_PORT": "2525", "EMAIL_USE_TLS": "0",
	}}
	settings, err := emailSettings(context.Background(), reader, EmailSettings{
		Host: "fallback", Port: "587", UseTLS: "1", From: "Team <team@pace.test>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Host != "smtp.pace.test" || settings.Port != "2525" || settings.UseTLS != "0" {
		t.Fatalf("settings = %#v", settings)
	}
	// Keys the instance does not define keep the environment fallback.
	if settings.From != "Team <team@pace.test>" {
		t.Fatalf("from = %q", settings.From)
	}
	if _, err := emailSettings(context.Background(), fakeConfiguration{values: map[string]string{"EMAIL_PORT": "abc"}}, EmailSettings{}); err == nil {
		t.Fatal("a non numeric port should be rejected")
	}
}

func TestMessageCarriesBothAlternatives(t *testing.T) {
	message, err := buildMultipartMessage("Team <team@pace.test>", "to@pace.test", "Subject", "plain", "<p>html</p>")
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	for _, fragment := range []string{
		"From: Team <team@pace.test>", "To: to@pace.test", "Subject: Subject",
		"multipart/alternative", `Content-Type: text/plain; charset="utf-8"`,
		`Content-Type: text/html; charset="utf-8"`, "plain", "<p>html</p>",
	} {
		if !strings.Contains(message, fragment) {
			t.Errorf("message is missing %q", fragment)
		}
	}
	if got := envelopeAddress("Team Plane <team@mailer.plane.so>"); got != "team@mailer.plane.so" {
		t.Fatalf("envelope address = %q", got)
	}
	if got := envelopeAddress("plain@pace.test"); got != "plain@pace.test" {
		t.Fatalf("envelope address = %q", got)
	}
}

func TestNonASCIISubjectIsEncoded(t *testing.T) {
	if got := encodeHeader("plain subject"); got != "plain subject" {
		t.Fatalf("ascii subject = %q", got)
	}
	got := encodeHeader("你好")
	if !strings.HasPrefix(got, "=?utf-8?b?") || !strings.HasSuffix(got, "?=") {
		t.Fatalf("encoded subject = %q", got)
	}
}

type recordingMailer struct {
	to, subject, text, html string
	attachments             []recordedAttachment
}

func (mailer *recordingMailer) SendAttachment(_ context.Context, _ EmailSettings, to, subject, text, filename, contentType string, content []byte) error {
	mailer.attachments = append(mailer.attachments, recordedAttachment{
		to: to, subject: subject, text: text, filename: filename, contentType: contentType, content: string(content),
	})
	return nil
}

// recordedAttachment is one export email, kept whole so a test can read the file that went with it.
type recordedAttachment struct {
	to, subject, text, filename, contentType, content string
}

func (mailer *recordingMailer) Send(_ context.Context, _ EmailSettings, to, subject, text, html string) error {
	mailer.to, mailer.subject, mailer.text, mailer.html = to, subject, text, html
	return nil
}

type fakeConfiguration struct{ values map[string]string }

func (config fakeConfiguration) ConfigurationValue(_ context.Context, key, fallback string) (string, error) {
	if value, ok := config.values[key]; ok {
		return value, nil
	}
	return fallback, nil
}

func TestCleanupTaskNamesMatchTheBeatSchedule(t *testing.T) {
	// plane/celery.py schedules these five by name; a typo here would mean the
	// Go worker silently never runs them.
	for _, name := range []string{
		"plane.bgtasks.cleanup_task.delete_api_logs",
		"plane.bgtasks.cleanup_task.delete_email_notification_logs",
		"plane.bgtasks.cleanup_task.delete_page_versions",
		"plane.bgtasks.cleanup_task.delete_issue_description_versions",
		"plane.bgtasks.cleanup_task.delete_webhook_logs",
		"plane.bgtasks.file_asset_task.delete_unuploaded_file_asset",
		"plane.bgtasks.exporter_expired_task.delete_old_s3_link",
		"plane.bgtasks.issue_automation_task.archive_and_close_old_issues",
		"plane.bgtasks.email_notification_task.stack_email_notification",
	} {
		found := false
		for _, migrated := range MigratedTaskNames() {
			if migrated == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("beat task %q is not routed to the Go worker", name)
		}
	}
}

func TestMaintenanceTasksRegisterEveryName(t *testing.T) {
	consumer := NewConsumer("", "", nil)
	NewEmailTasks(nil, EmailSettings{}, nil, &recordingMailer{}, nil).Register(consumer)
	NewMaintenanceTasks(nil, DefaultRetentionSettings(), nil).Register(consumer)
	deletions, err := NewDeletionTasks(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	deletions.Register(consumer)
	NewVersionTasks(nil, nil).Register(consumer)
	NewAssetTasks(nil, nil, nil).Register(consumer)
	NewLinkTasks(nil, httpsafe.Settings{}, nil).Register(consumer)
	NewNightlyTasks(nil, nil, nil, nil).Register(consumer)
	NewModelActivityTasks(nil, nil).Register(consumer)
	NewIssueActivityTasks(nil, nil, nil, nil).Register(consumer)
	NewNotificationTasks(nil, nil).Register(consumer)
	NewWebhookTasks(nil, httpsafe.Settings{}, nil, nil, nil).Register(consumer)
	NewEmailStackTasks(nil, nil, nil).Register(consumer)
	emailSend, err := NewEmailSendTasks(nil, nil, EmailSettings{}, nil, &recordingMailer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	emailSend.Register(consumer)
	NewExportTasks(nil, nil, nil, false).Register(consumer)
	NewAnalyticExportTasks(nil, EmailSettings{}, nil, nil, nil).Register(consumer)
	NewCopyAssetTasks(nil, nil, "", nil).Register(consumer)
	NewVersionSyncTasks(nil, nil, nil).Register(consumer)
	NewDummyDataTasks(nil, nil).Register(consumer)
	NewWorkspaceSeedTasks(nil, "", nil).Register(consumer)
	NewTelemetryTasks(nil, "", nil).Register(consumer)
	NewProjectInvitationTasks(nil, EmailSettings{}, nil, nil, nil).Register(consumer)
	NewAPILogTasks(nil, nil).Register(consumer)
	registered := map[string]bool{}
	for _, name := range consumer.TaskNames() {
		registered[name] = true
	}
	for _, name := range MigratedTaskNames() {
		if !registered[name] {
			t.Errorf("task %q is routed to the Go queue but has no handler", name)
		}
	}
	if len(registered) != len(MigratedTaskNames()) {
		t.Fatalf("registered %d handlers but route %d task names", len(registered), len(MigratedTaskNames()))
	}
}

func TestRetentionDefaultsMatchDjango(t *testing.T) {
	settings := DefaultRetentionSettings()
	if settings.APIActivityLogDays != 14 || settings.EmailLogDays != 7 || settings.WebhookLogDays != 14 {
		t.Fatalf("retention defaults = %#v", settings)
	}
}

func TestNullableIDKeepsEmptyOutOfUUIDColumns(t *testing.T) {
	if nullableID("") != nil {
		t.Fatal("an empty identifier must become NULL")
	}
	if nullableID("abc") != "abc" {
		t.Fatal("a present identifier must be kept")
	}
}

// TestHeaderValuesCannotCarryNewlines covers the protection Django got from EmailMultiAlternatives and this package had to reimplement. The invitation subjects are built from a display name, a project name and a workspace name, all of which the person being invited did not choose, so a CR or LF in one of them would have ended the Subject header and started whatever header came next.
func TestHeaderValuesCannotCarryNewlines(t *testing.T) {
	injected := "Someone\r\nBcc: attacker@example.test"

	if _, err := buildMultipartMessage("team@pace.test", "to@pace.test", injected, "plain", "<p>html</p>"); err == nil {
		t.Error("a subject carrying CRLF was accepted")
	} else {
		var bad BadHeaderError
		if !errors.As(err, &bad) || bad.Header != "Subject" {
			t.Errorf("error = %v, want a BadHeaderError naming Subject", err)
		}
	}

	for name, args := range map[string][3]string{
		"From":    {injected, "to@pace.test", "Subject"},
		"To":      {"team@pace.test", injected, "Subject"},
		"Subject": {"team@pace.test", "to@pace.test", injected},
	} {
		if _, err := buildMultipartMessage(args[0], args[1], args[2], "plain", "<p>html</p>"); err == nil {
			t.Errorf("%s carrying CRLF was accepted", name)
		}
	}

	// A bare LF is as good as a CRLF to a mail transport, so it is refused too.
	if _, err := buildMultipartMessage("team@pace.test", "to@pace.test", "Hi\nBcc: x@y.test", "plain", ""); err == nil {
		t.Error("a subject carrying a bare LF was accepted")
	}

	// The attachment builder writes the filename and the content type into headers of their own.
	if _, err := buildAttachmentMessage("team@pace.test", "to@pace.test", "Subject", "plain", injected, "text/csv", []byte("a")); err == nil {
		t.Error("a filename carrying CRLF was accepted")
	}
	if _, err := buildAttachmentMessage("team@pace.test", "to@pace.test", "Subject", "plain", "export.csv", injected, []byte("a")); err == nil {
		t.Error("a content type carrying CRLF was accepted")
	}

	// Nothing above should have made ordinary values harder to send.
	if _, err := buildAttachmentMessage("team@pace.test", "to@pace.test", "Subject", "plain", "export.csv", "text/csv", []byte("a")); err != nil {
		t.Errorf("an ordinary attachment message was refused: %v", err)
	}
}
