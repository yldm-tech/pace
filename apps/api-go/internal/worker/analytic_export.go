package worker

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/project"
	"gorm.io/gorm"
)

// AnalyticExportTask emails the caller a spreadsheet of the chart they were looking at.
const AnalyticExportTask = "plane.bgtasks.analytic_plot_export.analytic_export_task"

// ExportAnalyticsToCSVEmailTask emails a spreadsheet somebody else already assembled. Nothing in this edition queues it; it is here because the task name exists and a message carrying it would otherwise sit unhandled.
const ExportAnalyticsToCSVEmailTask = "plane.bgtasks.analytic_plot_export.export_analytics_to_csv_email"

// AnalyticExportTasks builds those spreadsheets and sends them.
type AnalyticExportTasks struct {
	db       *gorm.DB
	settings EmailSettings
	config   ConfigurationReader
	mailer   Mailer
	logger   *slog.Logger
	clock    func() time.Time
}

func NewAnalyticExportTasks(db *gorm.DB, settings EmailSettings, config ConfigurationReader, mailer Mailer, logger *slog.Logger) *AnalyticExportTasks {
	return &AnalyticExportTasks{db: db, settings: settings, config: config, mailer: mailer, logger: logger, clock: time.Now}
}

func (tasks *AnalyticExportTasks) Register(consumer *Consumer) {
	consumer.Register(AnalyticExportTask, tasks.analyticExport)
	consumer.Register(ExportAnalyticsToCSVEmailTask, tasks.analyticsCSVEmail)
}

// analyticExport reproduces analytic_export_task.
//
// Nothing it can go wrong at is reported anywhere. The whole body sits in one try, and the except logs and returns — so an axis the chart does not know, a filter the ORM refuses, or a mail server that will not answer all end the same way: the person who asked waits for an email that never arrives. That is reproduced rather than corrected, which is why the errors here are logged and swallowed rather than returned.
func (tasks *AnalyticExportTasks) analyticExport(ctx context.Context, arguments []any, keywords map[string]any) error {
	email := stringArgument(arguments, keywords, 0, "email")
	payload, _ := argument(arguments, keywords, 1, "data")
	slug := stringArgument(arguments, keywords, 2, "slug")
	data, _ := payload.(map[string]any)

	rows, err := project.AnalyticsExportRows(ctx, tasks.db, tasks.clock().UTC(), slug, data)
	if err != nil {
		tasks.logger.Warn("an analytics export could not be built", "slug", slug, "error", err)
		return nil
	}
	if err := tasks.sendExport(ctx, email, slug, rows); err != nil {
		tasks.logger.Warn("an analytics export could not be sent", "slug", slug, "error", err)
	}
	return nil
}

// analyticsCSVEmail reproduces export_analytics_to_csv_email: rows somebody else assembled, turned into the same file and sent the same way.
func (tasks *AnalyticExportTasks) analyticsCSVEmail(ctx context.Context, arguments []any, keywords map[string]any) error {
	payload, _ := argument(arguments, keywords, 0, "data")
	headers := stringListArgument(arguments, keywords, 1, "headers")
	keys := stringListArgument(arguments, keywords, 2, "keys")
	email := stringArgument(arguments, keywords, 3, "email")
	slug := stringArgument(arguments, keywords, 4, "slug")

	rows := [][]any{toAnyRow(headers)}
	items, _ := payload.([]any)
	for _, item := range items {
		record, _ := item.(map[string]any)
		row := make([]any, 0, len(keys))
		for _, key := range keys {
			// A key the record does not carry becomes an empty string rather than nothing.
			value, found := record[key]
			if !found {
				value = ""
			}
			row = append(row, value)
		}
		rows = append(rows, row)
	}
	if err := tasks.sendExport(ctx, email, slug, rows); err != nil {
		tasks.logger.Warn("an analytics export could not be sent", "slug", slug, "error", err)
	}
	return nil
}

func toAnyRow(values []string) []any {
	row := make([]any, 0, len(values))
	for _, value := range values {
		row = append(row, value)
	}
	return row
}

// analyticsExportSubject is the subject every one of these carries, whatever the chart was.
const analyticsExportSubject = "Your Export is ready"

// sendExport writes the rows into a csv and attaches it to the one email this sends.
//
// The message has no html part. Django renders the template only to turn it into plain text and then never attaches it, so what arrives is a text body with a spreadsheet beside it.
func (tasks *AnalyticExportTasks) sendExport(ctx context.Context, email, slug string, rows [][]any) error {
	html, err := renderEmail("emails/exports/analytics.html", map[string]any{})
	if err != nil {
		return err
	}
	settings, err := emailSettings(ctx, tasks.config, tasks.settings)
	if err != nil {
		return err
	}
	return tasks.mailer.SendAttachment(ctx, settings, email, analyticsExportSubject,
		plainTextFromHTML(html), slug+"-analytics.csv", "text/csv", []byte(analyticsCSV(rows)))
}

// analyticsCSV is generate_csv_from_rows: every field quoted, whatever it holds, and every record ended with CRLF.
//
// The quoting is what makes this writer different from the export one. Python is asked for QUOTE_ALL here and for the default QUOTE_MINIMAL there, so the same value is written two ways depending on which export produced it.
func analyticsCSV(rows [][]any) string {
	var buffer bytes.Buffer
	for _, row := range rows {
		for index, field := range row {
			if index > 0 {
				buffer.WriteByte(',')
			}
			text := analyticsCSVField(field)
			buffer.WriteByte('"')
			buffer.WriteString(strings.ReplaceAll(text, `"`, `""`))
			buffer.WriteByte('"')
		}
		buffer.WriteString("\r\n")
	}
	return buffer.String()
}

// analyticsCSVField renders one value, sanitised first and rendered after — which is the order upstream uses, and the reason a negative number is not mistaken for a formula while a string that starts with a minus sign is.
func analyticsCSVField(value any) string {
	return csvText(sanitizeCSVValue(value))
}
