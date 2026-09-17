package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// ProcessLogsTask keeps a record of every request made with an api key, which is what an installation's administrator reads to see what a key has been doing.
const ProcessLogsTask = "plane.bgtasks.logger_task.process_logs"

// APILogTasks writes those records.
type APILogTasks struct {
	db     *gorm.DB
	logger *slog.Logger
	clock  func() time.Time
}

func NewAPILogTasks(db *gorm.DB, logger *slog.Logger) *APILogTasks {
	return &APILogTasks{db: db, logger: logger, clock: time.Now}
}

func (tasks *APILogTasks) Register(consumer *Consumer) {
	consumer.Register(ProcessLogsTask, tasks.processLogs)
}

// processLogs reproduces process_logs: one row per request, and a row that cannot be written is logged rather than retried.
//
// The task takes whatever keywords it is given and ignores the ones it does not know, which is how a rolling deploy keeps working — a release that sent an argument this one has never heard of does not fail here.
func (tasks *APILogTasks) processLogs(ctx context.Context, arguments []any, keywords map[string]any) error {
	data, _ := argument(arguments, keywords, 0, "log_data")
	entry, ok := data.(map[string]any)
	if !ok {
		tasks.logger.Warn("an api activity log arrived in a shape this does not know")
		return nil
	}

	identifier, err := newTaskUUID()
	if err != nil {
		return err
	}
	row := apiLogRow(entry, identifier, tasks.clock().UTC())
	if err := tasks.db.WithContext(ctx).Table("api_activity_logs").Create(row).Error; err != nil {
		// Django logs the failure and answers False, which nobody reads. A row that cannot be written is not worth failing the delivery for.
		tasks.logger.Warn("an api activity log could not be written", "error", err)
	}
	return nil
}

// apiLogRow is the row one record becomes. The three columns the model declares as not null are written as the empty string when they were never given, which is what a model instance built without them holds; the rest stay null.
func apiLogRow(entry map[string]any, identifier any, now time.Time) map[string]any {
	return map[string]any{
		"id": identifier, "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"token_identifier": activityTextOrEmpty(entry["token_identifier"]),
		"path":             activityTextOrEmpty(entry["path"]),
		"method":           activityTextOrEmpty(entry["method"]),
		"query_params":     nullableText(entry["query_params"]),
		"headers":          nullableText(entry["headers"]),
		"body":             nullableText(entry["body"]),
		"response_code":    statusCodeOf(entry["response_code"]),
		"response_body":    nullableText(entry["response_body"]),
		"ip_address":       nullableText(entry["ip_address"]),
		"user_agent":       nullableText(entry["user_agent"]),
	}
}

// nullableText keeps a column that was never given as null rather than as an empty string.
func nullableText(value any) any {
	if value == nil {
		return nil
	}
	return activityTextOrEmpty(value)
}

// statusCodeOf reads the status a request answered with, whatever shape the json decoder left it in.
func statusCodeOf(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		if number, err := typed.Int64(); err == nil {
			return int(number)
		}
	case string:
		if number, err := strconv.Atoi(typed); err == nil {
			return number
		}
	}
	return 0
}
