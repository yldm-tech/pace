package worker

import (
	"context"
	"strings"
	"testing"
)

// The analytics csv quotes every field, whatever it holds, which is the opposite of what the work item export does with the same values.
func TestTheAnalyticsCSVQuotesEveryField(t *testing.T) {
	rows := [][]any{
		{"Priority", "Issue Count", "urgent"},
		{"=danger", int64(3), "0"},
		{"none", nil, 4.0},
		{`a"b`, int64(-5), "-x"},
	}
	// Checked against generate_csv_from_rows itself rather than written from the documentation.
	want := "\"Priority\",\"Issue Count\",\"urgent\"\r\n" +
		"\"'=danger\",\"3\",\"0\"\r\n" +
		"\"none\",\"\",\"4.0\"\r\n" +
		"\"a\"\"b\",\"-5\",\"'-x\"\r\n"
	if got := analyticsCSV(rows); got != want {
		t.Errorf("the csv is\n%q\nrather than\n%q", got, want)
	}
}

// A number is sanitised as a number, so a negative count keeps its minus sign while a string that merely starts with one is quoted out of harm's way.
func TestANegativeNumberIsNotAFormula(t *testing.T) {
	if got := analyticsCSVField(int64(-5)); got != "-5" {
		t.Errorf("a negative count is %q", got)
	}
	if got := analyticsCSVField(-5.5); got != "-5.5" {
		t.Errorf("a negative estimate is %q", got)
	}
	if got := analyticsCSVField("-5"); got != "'-5" {
		t.Errorf("a string that starts with a minus is %q", got)
	}
	// A float always keeps its decimal point, which is what python's str() of one leaves behind.
	if got := analyticsCSVField(4.0); got != "4.0" {
		t.Errorf("a whole estimate is %q", got)
	}
	if got := analyticsCSVField(nil); got != "" {
		t.Errorf("a missing measure is %q", got)
	}
}

// The email carries the spreadsheet as an attachment and no html part, which is what Django's attach() without attach_alternative produces.
func TestTheExportEmailCarriesTheSpreadsheet(t *testing.T) {
	mailer := &recordingMailer{}
	tasks := NewAnalyticExportTasks(nil, EmailSettings{From: "team@example.test"}, nil, mailer, nil)
	rows := [][]any{{"Priority", "Issue Count"}, {"urgent", int64(2)}}
	if err := tasks.sendExport(context.Background(), "ada@example.test", "acme", rows); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if len(mailer.attachments) != 1 {
		t.Fatalf("sent %d messages", len(mailer.attachments))
	}
	sent := mailer.attachments[0]
	if sent.to != "ada@example.test" || sent.subject != "Your Export is ready" {
		t.Errorf("the message is %#v", sent)
	}
	if sent.filename != "acme-analytics.csv" || sent.contentType != "text/csv" {
		t.Errorf("the attachment is %s (%s)", sent.filename, sent.contentType)
	}
	if !strings.Contains(sent.content, `"urgent","2"`) {
		t.Errorf("the attachment holds %q", sent.content)
	}
	// The body is the template with its tags stripped, so it reads as words rather than as markup.
	if strings.Contains(sent.text, "<") || strings.TrimSpace(sent.text) == "" {
		t.Errorf("the body is %q", sent.text)
	}
	if len(mailer.html) != 0 {
		t.Error("an html alternative was sent, and this message has none")
	}
}

// Rows somebody else assembled go through the same file and the same email, and a key a record does not carry becomes an empty cell.
func TestRowsAssembledElsewhere(t *testing.T) {
	mailer := &recordingMailer{}
	tasks := NewAnalyticExportTasks(nil, EmailSettings{}, nil, mailer, nil)
	err := tasks.analyticsCSVEmail(context.Background(), nil, map[string]any{
		"data": []any{
			map[string]any{"name": "One", "count": int64(3)},
			map[string]any{"name": "Two"},
		},
		"headers": []any{"Name", "Count"},
		"keys":    []any{"name", "count"},
		"email":   "ada@example.test",
		"slug":    "acme",
	})
	if err != nil {
		t.Fatalf("running the task: %v", err)
	}
	if len(mailer.attachments) != 1 {
		t.Fatalf("sent %d messages", len(mailer.attachments))
	}
	want := "\"Name\",\"Count\"\r\n\"One\",\"3\"\r\n\"Two\",\"\"\r\n"
	if got := mailer.attachments[0].content; got != want {
		t.Errorf("the attachment is\n%q\nrather than\n%q", got, want)
	}
}
