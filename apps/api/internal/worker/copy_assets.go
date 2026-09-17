package worker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api/internal/soup"
	"gorm.io/gorm"
)

// CopyAssetsTask duplicates the pictures in a description when the thing carrying it is duplicated, so the copy points at its own files rather than at the original's.
const CopyAssetsTask = "plane.bgtasks.copy_s3_object.copy_s3_objects_of_description_and_assets"

// AssetCopyStore is the part of object storage this needs: the bucket copies the bytes from one key to another without them passing through here.
type AssetCopyStore interface {
	CopyObject(ctx context.Context, sourceKey, destinationKey string) error
}

// CopyAssetTasks duplicates those pictures.
type CopyAssetTasks struct {
	db      *gorm.DB
	store   AssetCopyStore
	client  *http.Client
	liveURL string
	logger  *slog.Logger
	clock   func() time.Time
}

func NewCopyAssetTasks(db *gorm.DB, store AssetCopyStore, liveURL string, logger *slog.Logger) *CopyAssetTasks {
	return &CopyAssetTasks{
		db: db, store: store, liveURL: liveURL, logger: logger, clock: time.Now,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (tasks *CopyAssetTasks) Register(consumer *Consumer) {
	consumer.Register(CopyAssetsTask, tasks.copyAssets)
}

// copyAssets reproduces copy_s3_objects_of_description_and_assets.
//
// Four steps: read the asset ids out of the description, duplicate each asset in the bucket and in the table, rewrite the description to point at the copies, and ask the live server to turn the new html into the editor's own two representations of it. The whole thing sits in one try upstream and its except logs and returns, so a copy whose pictures could not be duplicated simply keeps pointing at the original's — reproduced rather than corrected.
func (tasks *CopyAssetTasks) copyAssets(ctx context.Context, arguments []any, keywords map[string]any) error {
	entityName := stringArgument(arguments, keywords, 0, "entity_name")
	entityID := stringArgument(arguments, keywords, 1, "entity_identifier")
	projectID := stringArgument(arguments, keywords, 2, "project_id")
	// user_id is read and never used, the same way it is upstream: create() is handed it and BaseModel.save discards it.
	_ = stringArgument(arguments, keywords, 4, "user_id")

	table, known := copyAssetTables[entityName]
	if !known {
		tasks.logger.Warn("an asset copy named an entity it does not know", "entity", entityName)
		return nil
	}

	var entity struct {
		ID              string `gorm:"column:id"`
		WorkspaceID     string `gorm:"column:workspace_id"`
		DescriptionHTML string `gorm:"column:description_html"`
	}
	err := tasks.db.WithContext(ctx).Table(table).Where("id = ?", entityID).
		Select("id, workspace_id, description_html").Take(&entity).Error
	if err != nil {
		tasks.logger.Warn("an asset copy named something that is not there", "entity", entityName, "id", entityID, "error", err)
		return nil
	}

	document := soup.Parse(entity.DescriptionHTML)
	assetIDs := []string{}
	for _, element := range document.FindAll("image-component") {
		if source, found := element.Get("src"); found && source != "" {
			assetIDs = append(assetIDs, source)
		}
	}

	copies, err := tasks.duplicateAssets(ctx, entity.WorkspaceID, entityID, projectID, assetIDs)
	if err != nil {
		tasks.logger.Warn("the assets of a description could not be duplicated", "id", entityID, "error", err)
		return nil
	}

	// The description is rewritten and saved even when nothing was duplicated, which is how a description that never held a picture still comes back through bs4's reading of it.
	for _, element := range document.FindAll("image-component") {
		source, _ := element.Get("src")
		for _, copied := range copies {
			if source == copied.oldID {
				element.Set("src", copied.newID)
			}
		}
	}
	updated := document.Render()
	if err := tasks.saveDescription(ctx, table, entityID, map[string]any{"description_html": updated}); err != nil {
		tasks.logger.Warn("a rewritten description could not be saved", "id", entityID, "error", err)
		return nil
	}

	converted, err := tasks.convertDocument(ctx, entityName, updated)
	if err != nil {
		tasks.logger.Warn("the live server would not convert a description", "id", entityID, "error", err)
		return nil
	}
	if len(converted) == 0 {
		return nil
	}
	if err := tasks.saveConverted(ctx, table, entityID, converted); err != nil {
		tasks.logger.Warn("a converted description could not be saved", "id", entityID, "error", err)
	}
	return nil
}

// copyAssetTables are the two things whose descriptions carry pictures. An entity_name that is neither raises upstream, which the task's own except swallows.
var copyAssetTables = map[string]string{"PAGE": "pages", "ISSUE": "issues"}

// copyAssetOwnerColumns is get_entity_id_field: which column on the new asset row points back at the thing the description belongs to. An entity type the table does not name leaves every one of them null, so the copy is owned by nothing.
var copyAssetOwnerColumns = map[string]string{
	"WORKSPACE_LOGO":          "workspace_id",
	"PROJECT_COVER":           "project_id",
	"USER_AVATAR":             "user_id",
	"USER_COVER":              "user_id",
	"ISSUE_ATTACHMENT":        "issue_id",
	"ISSUE_DESCRIPTION":       "issue_id",
	"PAGE_DESCRIPTION":        "page_id",
	"COMMENT_DESCRIPTION":     "comment_id",
	"DRAFT_ISSUE_DESCRIPTION": "draft_issue_id",
}

// assetCopy is one duplication: the id the description pointed at and the id it should point at now.
type assetCopy struct{ oldID, newID string }

// duplicateAssets is copy_assets: a new row and a new object for each picture the description named.
//
// The originals are looked up by workspace and project as well as by id, so a description that names a picture belonging to another project simply copies nothing for it and keeps pointing at the original.
func (tasks *CopyAssetTasks) duplicateAssets(ctx context.Context, workspaceID, entityID, projectID string, assetIDs []string) ([]assetCopy, error) {
	copies := []assetCopy{}
	if len(assetIDs) == 0 {
		return copies, nil
	}
	var originals []struct {
		ID              string  `gorm:"column:id"`
		Asset           string  `gorm:"column:asset"`
		Size            *int64  `gorm:"column:size"`
		Attributes      []byte  `gorm:"column:attributes"`
		EntityType      *string `gorm:"column:entity_type"`
		StorageMetadata []byte  `gorm:"column:storage_metadata"`
	}
	err := tasks.db.WithContext(ctx).Table("file_assets").
		Where("workspace_id = ? AND project_id = ? AND id IN ? AND deleted_at IS NULL", workspaceID, projectID, assetIDs).
		Select("id, asset, size, attributes, entity_type, storage_metadata").Scan(&originals).Error
	if err != nil {
		return nil, err
	}

	newIDs := make([]string, 0, len(originals))
	for _, original := range originals {
		attributes := map[string]any{}
		_ = json.Unmarshal(original.Attributes, &attributes)
		// Only the three the editor needs are carried over; anything else the original held is dropped.
		carried := map[string]any{
			"name": attributes["name"], "type": attributes["type"], "size": attributes["size"],
		}
		carriedJSON, err := json.Marshal(carried)
		if err != nil {
			return nil, err
		}
		destination := workspaceID + "/" + strings.ReplaceAll(uuid.NewString(), "-", "") + "-" + assetNameOf(attributes["name"])

		now := tasks.clock().UTC()
		identifier := uuid.NewString()
		row := map[string]any{
			"id": identifier, "created_at": now, "updated_at": now,
			// The user who asked for the copy is handed to create() and then thrown away: BaseModel.save reads the current user out of crum instead, and a worker has none. So the copy is owned by nobody.
			"created_by_id": nil, "updated_by_id": nil,
			"attributes": string(carriedJSON), "asset": destination, "size": original.Size,
			"workspace_id": workspaceID, "project_id": projectID,
			"entity_type": original.EntityType, "storage_metadata": storageMetadataOrNull(original.StorageMetadata),
			"is_deleted": false, "is_archived": false, "is_uploaded": false,
		}
		if column, known := copyAssetOwnerColumns[stringOrEmpty(original.EntityType)]; known {
			row[column] = entityID
		}
		if err := tasks.db.WithContext(ctx).Table("file_assets").Create(row).Error; err != nil {
			return nil, err
		}
		if tasks.store != nil {
			if err := tasks.store.CopyObject(ctx, original.Asset, destination); err != nil {
				return nil, err
			}
		}
		copies = append(copies, assetCopy{oldID: original.ID, newID: identifier})
		newIDs = append(newIDs, identifier)
	}
	if len(newIDs) > 0 {
		// The copies are marked uploaded in one statement after the fact, which is what keeps a half-finished copy out of the sweep that deletes unuploaded assets.
		err := tasks.db.WithContext(ctx).Table("file_assets").Where("id IN ?", newIDs).
			Update("is_uploaded", true).Error
		if err != nil {
			return nil, err
		}
	}
	return copies, nil
}

// assetNameOf renders the original's name into the new key. A row whose attributes carry no name puts the literal None there, which is what python's f-string does with one.
func assetNameOf(value any) string {
	if value == nil {
		return "None"
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "None"
	}
	return string(encoded)
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func storageMetadataOrNull(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

// convertDocument is sync_with_external_service: the live server turns the html into the two representations the editor reads.
//
// It is the one piece of this that reaches outside the installation. With no live server configured it answers nothing and the description keeps whatever json and binary it already had, which upstream does too — the copy then opens with the original's content until somebody edits it.
func (tasks *CopyAssetTasks) convertDocument(ctx context.Context, entityName, descriptionHTML string) (map[string]any, error) {
	if strings.TrimSpace(tasks.liveURL) == "" {
		return nil, nil
	}
	variant := "document"
	if entityName == "PAGE" {
		variant = "rich"
	}
	body, err := json.Marshal(map[string]any{"description_html": descriptionHTML, "variant": variant})
	if err != nil {
		return nil, err
	}
	endpoint, err := normalizeURLPath(strings.TrimSuffix(tasks.liveURL, "/") + "/convert-document/")
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := tasks.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		// Any other status is not an error upstream either; requests only raises when the call itself fails.
		return nil, nil
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	converted := map[string]any{}
	if err := json.Unmarshal(payload, &converted); err != nil {
		return nil, err
	}
	return converted, nil
}

// saveConverted writes the two representations the live server answered with. The binary arrives base64 encoded and is stored decoded.
func (tasks *CopyAssetTasks) saveConverted(ctx context.Context, table, entityID string, converted map[string]any) error {
	descriptionJSON, err := json.Marshal(converted["description_json"])
	if err != nil {
		return err
	}
	encoded, _ := converted["description_binary"].(string)
	binary, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return errors.New("the live server answered with a description binary that is not base64")
	}
	return tasks.saveDescription(ctx, table, entityID, map[string]any{
		"description_json": string(descriptionJSON), "description_binary": binary,
	})
}

// saveDescription is entity.save(), which does three things beyond writing the columns it was given.
//
// It recomputes description_stripped from the html, because both Page.save and Issue.save do. It blanks created_by and updated_by, because BaseModel.save reads the current user out of crum and a worker has none — so duplicating a page forgets who made it. And it moves updated_at, which is an auto_now column. All three are reproduced.
func (tasks *CopyAssetTasks) saveDescription(ctx context.Context, table, entityID string, columns map[string]any) error {
	var current struct {
		DescriptionHTML string `gorm:"column:description_html"`
	}
	err := tasks.db.WithContext(ctx).Table(table).Where("id = ?", entityID).
		Select("description_html").Take(&current).Error
	if err != nil {
		return err
	}
	html := current.DescriptionHTML
	if written, given := columns["description_html"].(string); given {
		html = written
	}
	columns["description_stripped"] = strippedHTML(html)
	columns["created_by_id"] = nil
	columns["updated_by_id"] = nil
	columns["updated_at"] = tasks.clock().UTC()
	return tasks.db.WithContext(ctx).Table(table).Where("id = ?", entityID).Updates(columns).Error
}

var repeatedSlashes = regexp.MustCompile(`/+`)

// normalizeURLPath is plane.utils.url.normalize_url_path: repeated slashes in the path become one, and nothing else is touched.
func normalizeURLPath(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	parsed.Path = repeatedSlashes.ReplaceAllString(parsed.Path, "/")
	return parsed.String(), nil
}
