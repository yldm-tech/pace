package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The three tasks that keep a description's history, all of them queued by the Go API and until now run by the Python worker.
const (
	PageTransactionTask         = "plane.bgtasks.page_transaction_task.page_transaction"
	TrackPageVersionTask        = "plane.bgtasks.page_version_task.track_page_version"
	IssueDescriptionVersionTask = "plane.bgtasks.issue_description_version_task.issue_description_version_task"
)

// versionWindow is how long an edit counts as a continuation of the last version rather than a new one. Both version tasks use the same ten minutes.
const versionWindow = 600 * time.Second

// pageVersionsKept is the cap track_page_version trims to, which it tests with a strict greater-than.
const pageVersionsKept = 20

// VersionTasks writes the history behind a description: the page log, the page versions and the work item description versions.
type VersionTasks struct {
	db     *gorm.DB
	logger *slog.Logger
	clock  func() time.Time
}

func NewVersionTasks(db *gorm.DB, logger *slog.Logger) *VersionTasks {
	return &VersionTasks{db: db, logger: logger, clock: time.Now}
}

func (tasks *VersionTasks) Register(consumer *Consumer) {
	consumer.Register(PageTransactionTask, tasks.pageTransaction)
	consumer.Register(TrackPageVersionTask, tasks.trackPageVersion)
	consumer.Register(IssueDescriptionVersionTask, tasks.issueDescriptionVersion)
}

// pageComponents are the two custom tags a page's description can carry and what each one logs.
//
// A mention logs what it points at; an image logs its source. That second one is why this task so often does nothing at all — see pageTransaction.
var pageComponents = []struct {
	tag        string
	entityName string
	identifier func(map[string]string) string
}{
	{tag: "mention-component", entityName: "", identifier: func(attributes map[string]string) string {
		return attributes["entity_identifier"]
	}},
	{tag: "image-component", entityName: "image", identifier: func(attributes map[string]string) string {
		return attributes["src"]
	}},
}

// pageTransaction reproduces page_transaction: it compares the two descriptions and writes a log row for every component that appeared, then takes away the rows for the ones that went.
//
// **An image almost always stops the whole task.** The row it would write puts the image's `src` into `entity_identifier`, and that column is a uuid — so a source that is a url raises before anything is written, the task swallows it, and neither the inserts nor the deletions happen. A page with one image therefore logs nothing about its mentions either. Reproduced rather than corrected: fixing it here would write rows the Python worker never wrote.
func (tasks *VersionTasks) pageTransaction(ctx context.Context, arguments []any, keywords map[string]any) error {
	newHTML := stringArgument(arguments, keywords, 0, "new_description_html")
	oldHTML := stringArgument(arguments, keywords, 1, "old_description_html")
	pageID := stringArgument(arguments, keywords, 2, "page_id")

	var page struct {
		WorkspaceID string `gorm:"column:workspace_id"`
	}
	err := tasks.db.WithContext(ctx).Table("pages").Select("workspace_id").
		Where("id = ? AND deleted_at IS NULL", pageID).Take(&page).Error
	if err != nil {
		// Page.DoesNotExist reaches its own except and the task returns.
		return nil
	}

	var existingLogs int64
	err = tasks.db.WithContext(ctx).Table("page_logs").
		Where("page_id = ? AND deleted_at IS NULL", pageID).Count(&existingLogs).Error
	if err != nil {
		return err
	}
	hasExistingLogs := existingLogs > 0

	now := tasks.clock().UTC()
	rows := []map[string]any{}
	removed := []string{}
	for _, component := range pageComponents {
		oldEntities := componentAttributes(oldHTML, component.tag)
		newEntities := componentAttributes(newHTML, component.tag)

		oldIDs := map[string]bool{}
		for _, entity := range oldEntities {
			if entity["id"] != "" {
				oldIDs[entity["id"]] = true
			}
		}
		newIDs := map[string]bool{}
		for _, entity := range newEntities {
			if entity["id"] != "" {
				newIDs[entity["id"]] = true
			}
		}
		for identifier := range oldIDs {
			if !newIDs[identifier] {
				removed = append(removed, identifier)
			}
		}

		for _, entity := range newEntities {
			transaction := entity["id"]
			// A component that was already there is only logged when the page has no log at all yet, which is how a page written before this task existed gets backfilled.
			if transaction == "" || (oldIDs[transaction] && hasExistingLogs) {
				continue
			}
			entityName := component.entityName
			if entityName == "" {
				entityName = entity["entity_name"]
			}
			identifier := component.identifier(entity)
			if !looksLikeUUID(transaction) || (identifier != "" && !looksLikeUUID(identifier)) {
				// Django would raise here and swallow it, which loses the whole batch rather than this one row.
				tasks.logger.Warn("page transaction abandoned: a component's identifier is not a uuid",
					"page", pageID, "transaction", transaction, "entity_identifier", identifier)
				return nil
			}
			rowID, err := newTaskUUID()
			if err != nil {
				return err
			}
			rows = append(rows, map[string]any{
				"id": rowID, "created_at": now, "updated_at": now,
				"transaction": transaction, "page_id": pageID,
				"entity_identifier": nullableID(identifier), "entity_name": entityName,
				"entity_type": nil, "workspace_id": page.WorkspaceID,
			})
		}
	}

	if len(rows) > 0 {
		// ignore_conflicts, so a log row that is already there is left as it is.
		err := tasks.db.WithContext(ctx).Table("page_logs").
			Clauses(clause.OnConflict{DoNothing: true}).Create(rows).Error
		if err != nil {
			return err
		}
	}
	if len(removed) > 0 {
		// A queryset delete is a soft one, and it is not scoped to this page — a transaction id that somehow appears on another page goes with it.
		err := tasks.db.WithContext(ctx).Table("page_logs").
			Where("transaction IN ? AND deleted_at IS NULL", removed).
			Update("deleted_at", now).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// trackPageVersion reproduces track_page_version: an edit within ten minutes of the last version, by the same person, rewrites that version rather than adding one.
//
// It reads description_json. The column is not called description -- Page has never had one, which is its own story: page_version_task.py assigns page.description into description_json on both of its write branches, so upstream raises AttributeError into log_exception every time and page_versions stays empty. That is not reproduced here, because the port had already gone further than Django gets: naming the column description made Postgres refuse the select outright, the error met a bare return, and the task then logged itself completed. A version is written now where Django would have written one had its attribute resolved.
//
// The rewrite does not move last_saved_at, so a run of quick edits keeps folding into the version the first of them made and the window is measured from that first edit rather than the last. The work item's copy of this task does move it, which is the one place the two differ.
func (tasks *VersionTasks) trackPageVersion(ctx context.Context, arguments []any, keywords map[string]any) error {
	pageID := stringArgument(arguments, keywords, 0, "page_id")
	existingInstance := stringArgument(arguments, keywords, 1, "existing_instance")
	userID := stringArgument(arguments, keywords, 2, "user_id")

	var page struct {
		WorkspaceID         string  `gorm:"column:workspace_id"`
		DescriptionJSON     []byte  `gorm:"column:description_json"`
		DescriptionHTML     string  `gorm:"column:description_html"`
		DescriptionStripped *string `gorm:"column:description_stripped"`
		DescriptionBinary   []byte  `gorm:"column:description_binary"`
	}
	err := tasks.db.WithContext(ctx).Table("pages").
		Select("workspace_id, description_json, description_html, description_stripped, description_binary").
		Where("id = ? AND deleted_at IS NULL", pageID).Take(&page).Error
	if err != nil {
		// Django's except catches the page that is not there and logs it. A bare return said nothing, which is how a select that could not run at all went unnoticed while the task reported itself completed.
		tasks.logger.Warn("track page version: the page could not be read", "page", pageID, "error", err)
		return nil
	}

	previous := map[string]any{}
	if existingInstance != "" {
		if err := json.Unmarshal([]byte(existingInstance), &previous); err != nil {
			// Django lets json.loads raise into the outer except, which logs and returns.
			tasks.logger.Warn("track page version: the previous state is not json", "page", pageID)
			return nil
		}
	}
	if text, _ := previous["description_html"].(string); text == page.DescriptionHTML {
		return nil
	}

	now := tasks.clock().UTC()
	var latest struct {
		ID          string    `gorm:"column:id"`
		OwnedByID   string    `gorm:"column:owned_by_id"`
		LastSavedAt time.Time `gorm:"column:last_saved_at"`
	}
	err = tasks.db.WithContext(ctx).Table("page_versions").
		Select("id, owned_by_id, last_saved_at").
		Where("page_id = ? AND deleted_at IS NULL", pageID).
		Order("last_saved_at DESC").Limit(1).Take(&latest).Error
	continues := err == nil && latest.ID != "" && latest.OwnedByID == userID &&
		now.Sub(latest.LastSavedAt) <= versionWindow

	if continues {
		// The model's save recomputes the stripped copy from the html rather than taking the page's, and update_fields keeps last_saved_at where it was.
		err = tasks.db.WithContext(ctx).Table("page_versions").Where("id = ?", latest.ID).
			Updates(map[string]any{
				"description_html": page.DescriptionHTML, "description_binary": page.DescriptionBinary,
				"description_json": jsonOrEmpty(page.DescriptionJSON), "description_stripped": strippedHTML(page.DescriptionHTML),
				"sub_pages_data": "{}", "updated_at": now,
			}).Error
		if err != nil {
			return err
		}
	} else {
		versionID, err := newTaskUUID()
		if err != nil {
			return err
		}
		// The worker has no current user, so the two audit columns are written empty however the row was built.
		err = tasks.db.WithContext(ctx).Table("page_versions").Create(map[string]any{
			"id": versionID, "created_at": now, "updated_at": now,
			"created_by_id": nil, "updated_by_id": nil,
			"page_id": pageID, "workspace_id": page.WorkspaceID,
			"description_json": jsonOrEmpty(page.DescriptionJSON), "description_html": page.DescriptionHTML,
			"description_binary": page.DescriptionBinary, "description_stripped": strippedHTML(page.DescriptionHTML),
			"owned_by_id": userID, "last_saved_at": now, "sub_pages_data": "{}",
		}).Error
		if err != nil {
			return err
		}
	}

	var count int64
	err = tasks.db.WithContext(ctx).Table("page_versions").
		Where("page_id = ? AND deleted_at IS NULL", pageID).Count(&count).Error
	if err != nil {
		return err
	}
	if count <= pageVersionsKept {
		return nil
	}
	var oldest struct {
		ID string `gorm:"column:id"`
	}
	err = tasks.db.WithContext(ctx).Table("page_versions").Select("id").
		Where("page_id = ? AND deleted_at IS NULL", pageID).
		Order("last_saved_at").Limit(1).Take(&oldest).Error
	if err != nil || oldest.ID == "" {
		return nil
	}
	return tasks.db.WithContext(ctx).Table("page_versions").Where("id = ?", oldest.ID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
}

// issueDescriptionVersion reproduces issue_description_version_task, which is the work item's copy of the page version task and differs from it in three ways.
//
// It moves last_saved_at when it rewrites a version, so the ten minutes are measured from the most recent edit rather than the first. It has no cap, so nothing is trimmed. And it never writes a stripped copy of its own — the one the work item already has is carried across.
func (tasks *VersionTasks) issueDescriptionVersion(ctx context.Context, arguments []any, keywords map[string]any) error {
	updatedIssue := stringArgument(arguments, keywords, 0, "updated_issue")
	issueID := stringArgument(arguments, keywords, 1, "issue_id")
	userID := stringArgument(arguments, keywords, 2, "user_id")
	creating := boolArgument(arguments, keywords, 3, "is_creating")

	var issue struct {
		WorkspaceID         string  `gorm:"column:workspace_id"`
		ProjectID           string  `gorm:"column:project_id"`
		CreatedByID         *string `gorm:"column:created_by_id"`
		UpdatedByID         *string `gorm:"column:updated_by_id"`
		DescriptionJSON     []byte  `gorm:"column:description_json"`
		DescriptionHTML     string  `gorm:"column:description_html"`
		DescriptionStripped *string `gorm:"column:description_stripped"`
		DescriptionBinary   []byte  `gorm:"column:description_binary"`
	}
	err := tasks.db.WithContext(ctx).Table("issues").
		Select(`workspace_id, project_id, created_by_id, updated_by_id,
			description_json, description_html, description_stripped, description_binary`).
		Where("id = ? AND deleted_at IS NULL", issueID).Take(&issue).Error
	if err != nil {
		return nil
	}

	previous := map[string]any{}
	if updatedIssue != "" {
		if err := json.Unmarshal([]byte(updatedIssue), &previous); err != nil {
			tasks.logger.Warn("issue description version: the previous state is not json", "issue", issueID)
			return nil
		}
	}
	if text, _ := previous["description_html"].(string); text == issue.DescriptionHTML && !creating {
		return nil
	}

	now := tasks.clock().UTC()
	var latest struct {
		ID          string    `gorm:"column:id"`
		OwnedByID   string    `gorm:"column:owned_by_id"`
		LastSavedAt time.Time `gorm:"column:last_saved_at"`
	}
	err = tasks.db.WithContext(ctx).Table("issue_description_versions").
		Select("id, owned_by_id, last_saved_at").
		Where("issue_id = ? AND deleted_at IS NULL", issueID).
		Order("last_saved_at DESC").Limit(1).Take(&latest).Error
	continues := err == nil && latest.ID != "" && latest.OwnedByID == userID &&
		now.Sub(latest.LastSavedAt) <= versionWindow

	if continues {
		return tasks.db.WithContext(ctx).Table("issue_description_versions").Where("id = ?", latest.ID).
			Updates(map[string]any{
				"description_json": jsonOrEmpty(issue.DescriptionJSON), "description_html": issue.DescriptionHTML,
				"description_binary": issue.DescriptionBinary, "description_stripped": issue.DescriptionStripped,
				"last_saved_at": now, "updated_at": now,
			}).Error
	}
	versionID, err := newTaskUUID()
	if err != nil {
		return err
	}
	// The classmethod copies the work item's audit users onto the row and the model's save then blanks them, because the worker has no current user. The blanks are what reaches the table.
	return tasks.db.WithContext(ctx).Table("issue_description_versions").Create(map[string]any{
		"id": versionID, "created_at": now, "updated_at": now,
		"created_by_id": nil, "updated_by_id": nil,
		"workspace_id": issue.WorkspaceID, "project_id": issue.ProjectID,
		"owned_by_id": userID, "last_saved_at": now, "issue_id": issueID,
		"description_binary": issue.DescriptionBinary, "description_html": issue.DescriptionHTML,
		"description_stripped": issue.DescriptionStripped, "description_json": jsonOrEmpty(issue.DescriptionJSON),
	}).Error
}

// componentAttributes finds every instance of one custom tag and reads its attributes, which is what BeautifulSoup's find_all does over the same html.
func componentAttributes(source, tag string) []map[string]string {
	entities := []map[string]string{}
	if source == "" {
		return entities
	}
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return entities
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == tag {
			attributes := map[string]string{}
			for _, attribute := range node.Attr {
				attributes[attribute.Key] = attribute.Val
			}
			entities = append(entities, attributes)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return entities
}

var versionTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

// strippedHTML is Django's strip_tags, which the page version's own save applies. Empty html leaves no copy rather than an empty one.
func strippedHTML(source string) any {
	if source == "" {
		return nil
	}
	return versionTagPattern.ReplaceAllString(source, "")
}

// jsonOrEmpty keeps a null json column readable as the empty object the model defaults to.
func jsonOrEmpty(value []byte) string {
	if len(value) == 0 {
		return "{}"
	}
	return string(value)
}

var versionUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}$`)

func looksLikeUUID(value string) bool { return versionUUIDPattern.MatchString(value) }

// boolArgument reads a flag the way Python's truthiness would.
func boolArgument(arguments []any, keywords map[string]any, index int, name string) bool {
	value, ok := argument(arguments, keywords, index, name)
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed != "" && typed != "False" && typed != "false" && typed != "0"
	case float64:
		return typed != 0
	}
	return true
}
