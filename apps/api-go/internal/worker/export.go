package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// IssueExportTask builds the zip of spreadsheets a work item export ends up as, and turns it into the link the person who asked for it downloads.
const IssueExportTask = "plane.bgtasks.export_task.issue_export_task"

// exportLinkExpiry is the week a download link is good for.
const exportLinkExpiry = 7 * 24 * time.Hour

// ExportStore is the part of object storage this task needs: somewhere to put the zip and a link to reach it by.
type ExportStore interface {
	PutObject(ctx context.Context, objectName, contentType string, payload []byte, publicRead bool) error
	PresignedObject(ctx context.Context, objectName string, expiry time.Duration) (string, error)
}

// ExportTasks builds work item exports.
type ExportTasks struct {
	db       *gorm.DB
	logger   *slog.Logger
	store    ExportStore
	clock    func() time.Time
	useMinio bool
}

func NewExportTasks(db *gorm.DB, logger *slog.Logger, store ExportStore, useMinio bool) *ExportTasks {
	return &ExportTasks{db: db, logger: logger, store: store, clock: time.Now, useMinio: useMinio}
}

func (tasks *ExportTasks) Register(consumer *Consumer) {
	consumer.Register(IssueExportTask, tasks.issueExport)
}

// exporterRow is the record of one export, read by the token the route handed over rather than by its id.
type exporterRow struct {
	ID            string `gorm:"column:id"`
	Token         string `gorm:"column:token"`
	Provider      string `gorm:"column:provider"`
	InitiatedByID string `gorm:"column:initiated_by_id"`
}

// issueExport reproduces issue_export_task.
//
// Everything it can fail at is caught in one place and written onto the record as a reason, because that is the only way the person waiting for the file learns that it is not coming. The one failure it cannot report is not finding the record at all: Django reads it again inside its own except block, so a token that names nothing raises a second time and the task simply dies.
func (tasks *ExportTasks) issueExport(ctx context.Context, arguments []any, keywords map[string]any) error {
	provider := stringArgument(arguments, keywords, 0, "provider")
	workspaceID := stringArgument(arguments, keywords, 1, "workspace_id")
	projectIDs := stringListArgument(arguments, keywords, 2, "project_ids")
	token := stringArgument(arguments, keywords, 3, "token_id")
	multiple := boolArgument(arguments, keywords, 4, "multiple")
	slug := stringArgument(arguments, keywords, 5, "slug")

	var record exporterRow
	err := tasks.db.WithContext(ctx).Table("exporters").Where("token = ? AND deleted_at IS NULL", token).
		Select("id, token, provider, initiated_by_id").Take(&record).Error
	if err != nil {
		return fmt.Errorf("issue export: no record for the token: %w", err)
	}
	if err := tasks.setStatus(ctx, record.ID, map[string]any{"status": "processing"}); err != nil {
		return err
	}

	if err := tasks.buildExport(ctx, record, provider, workspaceID, projectIDs, token, multiple, slug); err != nil {
		tasks.logger.Warn("a work item export could not be built", "token", token, "error", err)
		// The reason is written onto the record whatever went wrong, which is what the web app shows beside a failed export.
		return tasks.setStatus(ctx, record.ID, map[string]any{"status": "failed", "reason": err.Error()})
	}
	return nil
}

// buildExport does the work: read the work items, write the files, zip them, put the zip in the bucket and record the link.
func (tasks *ExportTasks) buildExport(ctx context.Context, record exporterRow, provider, workspaceID string, projectIDs []string, token string, multiple bool, slug string) error {
	format, known := exportFormats[provider]
	if !known {
		// DataExporter refuses an unknown format by name before it reads anything, and the view has already turned the three good ones away from here.
		return fmt.Errorf("Unsupported format: %s. Available: ['csv', 'json', 'xlsx']", provider)
	}

	files := make([][2]any, 0, len(projectIDs))
	if multiple {
		// One file per project, named after the project even when that project has nothing in it.
		for _, projectID := range projectIDs {
			content, err := tasks.exportContent(ctx, format, workspaceID, []string{projectID}, record.InitiatedByID)
			if err != nil {
				return err
			}
			files = append(files, [2]any{slug + "-" + projectID + "." + format.extension, content})
		}
	} else {
		content, err := tasks.exportContent(ctx, format, workspaceID, projectIDs, record.InitiatedByID)
		if err != nil {
			return err
		}
		files = append(files, [2]any{slug + "-" + workspaceID + "." + format.extension, content})
	}

	archive, err := zipExport(files)
	if err != nil {
		return err
	}
	if tasks.store == nil {
		return errors.New("object storage is not configured")
	}
	// The key carries only the first six characters of the token, so two exports of the same workspace on the same day overwrite each other unless their tokens differ early.
	key := workspaceID + "/export-" + slug + "-" + truncate(token, 6) + "-" + tasks.clock().UTC().Format("2006-01-02") + ".zip"
	if err := tasks.store.PutObject(ctx, key, "application/zip", archive, tasks.useMinio); err != nil {
		return err
	}
	link, err := tasks.store.PresignedObject(ctx, key, exportLinkExpiry)
	if err != nil {
		return err
	}
	if link == "" {
		// Django checks the link before recording it, and marks the export failed without a reason when there is none.
		return tasks.setStatus(ctx, record.ID, map[string]any{"status": "failed"})
	}
	return tasks.setStatus(ctx, record.ID, map[string]any{"status": "completed", "url": link, "key": key})
}

// exportFormat is one of the three formatters, reduced to what the task needs of it.
type exportFormat struct {
	extension string
	encode    func(rows []*orderedMap) (any, error)
}

var exportFormats = map[string]exportFormat{
	"csv":  {"csv", func(rows []*orderedMap) (any, error) { return encodeExportCSV(rows), nil }},
	"json": {"json", func(rows []*orderedMap) (any, error) { return encodeExportJSON(rows), nil }},
	"xlsx": {"xlsx", func(rows []*orderedMap) (any, error) { return encodeExportXLSX(rows) }},
}

// exportContent reads one file's worth of work items and encodes them.
func (tasks *ExportTasks) exportContent(ctx context.Context, format exportFormat, workspaceID string, projectIDs []string, memberID string) (any, error) {
	issues, err := loadExportIssues(ctx, tasks.db, workspaceID, projectIDs, memberID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	related, err := loadExportRelated(ctx, tasks.db, ids)
	if err != nil {
		return nil, err
	}
	rows := make([]*orderedMap, 0, len(issues))
	for _, issue := range issues {
		rows = append(rows, exportRow(issue, related))
	}
	return format.encode(rows)
}

// setStatus writes the columns Django names in update_fields, and only those: updated_at is left where it was, because a save that names its fields does not carry the auto_now column along.
func (tasks *ExportTasks) setStatus(ctx context.Context, id string, columns map[string]any) error {
	return tasks.db.WithContext(ctx).Table("exporters").Where("id = ?", id).Updates(columns).Error
}

func truncate(value string, length int) string {
	if len(value) <= length {
		return value
	}
	return value[:length]
}
