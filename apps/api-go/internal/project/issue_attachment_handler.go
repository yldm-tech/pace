package project

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
	"github.com/yldm-tech/pace/apps/api-go/internal/uploads"
	"gorm.io/gorm"
)

const attachmentEntityType = "ISSUE_ATTACHMENT"

func (handler *Handler) registerIssueAttachmentRoutes(router gin.IRouter) {
	const collection = "/api/assets/v2/workspaces/:slug/projects/:id/issues/:issue/attachments/"
	const detail = collection + ":asset/"
	router.POST(collection, handler.authenticatedIssueUUID(handler.issueAttachmentCreate))
	router.GET(collection, handler.authenticatedIssueUUID(handler.issueAttachmentList))
	router.GET(detail, handler.authenticatedIssueUUID(handler.issueAttachmentDownload))
	router.PATCH(detail, handler.authenticatedIssueUUID(handler.issueAttachmentMarkUploaded))
	router.DELETE(detail, handler.authenticatedIssueUUID(handler.issueAttachmentDestroy))
}

// issueAttachmentCreate reserves a row and hands back a policy the browser posts the file against, so the bytes never pass through the API.
func (handler *Handler) issueAttachmentCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	if handler.storage == nil {
		handler.internalError(c, errors.New("object storage is not configured"))
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var request struct {
		Name string          `json:"name"`
		Type string          `json:"type"`
		Size json.RawMessage `json:"size"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}
	name := uploads.SanitizeFilename(request.Name)
	if name == "" {
		// Django falls back to a placeholder rather than refusing a name it stripped to nothing.
		name = "unnamed"
	}
	if request.Type == "" || !uploads.AttachmentMimeTypes[request.Type] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid file type.", "status": false})
		return
	}
	limit := handler.fileSizeLimit()
	// An absent size means the limit, and any size is capped at it.
	size := limit
	if len(request.Size) > 0 {
		parsed, ok := parseAttachmentSize(request.Size)
		if !ok {
			handler.invalidDetail(c)
			return
		}
		size = parsed
	}
	if size > limit {
		size = limit
	}

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ?", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		// Django's unguarded Workspace.objects.get raises DoesNotExist, which becomes this 404.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	workspaceID := workspaceIDs[0]

	suffix, err := randomHex(16)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	assetKey := workspaceID + "/" + suffix + "-" + name
	assetID, err := newUUID()
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
	entityType := attachmentEntityType
	attributes, err := marshalUnescaped(map[string]any{"name": name, "type": request.Type, "size": size})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	asset := FileAsset{
		ID: assetID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		Attributes: auth.JSONValue(attributes), Asset: assetKey, Size: float64(size),
		WorkspaceID: &workspaceID, ProjectID: &projectID, IssueID: &issueID,
		EntityType: &entityType, StorageMetadata: auth.JSONValue([]byte("{}")),
	}
	if err := handler.db.WithContext(c.Request.Context()).Create(&asset).Error; err != nil {
		handler.internalError(c, err)
		return
	}

	upload, err := handler.storage.PresignedUpload(c.Request.Context(), assetKey, request.Type, size)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, gin.H{
		"upload_data": upload,
		"asset_id":    asset.ID,
		"attachment":  attachmentJSON(asset, slug),
		"asset_url":   attachmentURL(asset, slug),
	})
}

func (handler *Handler) issueAttachmentList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var assets []FileAsset
	// The list shows only what has finished uploading, so a row whose policy was never used stays hidden.
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where(`fa.issue_id = ? AND fa.project_id = ? AND w.slug = ? AND fa.entity_type = ?
			AND fa.is_uploaded = TRUE AND fa.deleted_at IS NULL`,
			issueID, projectID, slug, attachmentEntityType).
		Order("fa.created_at DESC").Find(&assets).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	serialized := make([]gin.H, 0, len(assets))
	for _, asset := range assets {
		serialized = append(serialized, attachmentJSON(asset, slug))
	}
	drf.Respond(c, http.StatusOK, serialized)
}

// issueAttachmentDownload answers with a redirect to a signed URL rather than the bytes, so the download comes straight from the bucket.
func (handler *Handler) issueAttachmentDownload(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	asset, found, err := handler.attachmentByID(c.Request.Context(), c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		// Django's unguarded .get raises DoesNotExist, which becomes this 404.
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	if !asset.IsUploaded {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The asset is not uploaded.", "status": false})
		return
	}
	if handler.storage == nil {
		handler.internalError(c, errors.New("object storage is not configured"))
		return
	}
	signed, err := handler.storage.PresignedDownload(c.Request.Context(), asset.Asset, "attachment", attachmentName(asset))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Redirect(http.StatusFound, signed)
}

// issueAttachmentMarkUploaded is what the browser calls once the bucket has the file.
func (handler *Handler) issueAttachmentMarkUploaded(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	asset, found, err := handler.attachmentByID(c.Request.Context(), c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	now := handler.clock().UTC()
	updates := map[string]any{"updated_at": now}
	if !asset.IsUploaded {
		// The activity carries the row as it stood before the flag moved, and is sent only the first time, so a repeated call does not announce the attachment twice.
		snapshot, err := json.Marshal(attachmentJSON(asset, c.Param("slug")))
		if err != nil {
			handler.internalError(c, err)
			return
		}
		currentInstance := string(snapshot)
		err = handler.publishIssueActivity(c, issueActivity{
			Type: "attachment.activity.created", CurrentInstance: &currentInstance,
			ActorID: user.ID, IssueID: c.Param("issue"), ProjectID: c.Param("id"),
			Notification: true, Origin: handler.origin(), Epoch: now,
		})
		if err != nil {
			handler.internalError(c, err)
			return
		}
		// created_by is deliberately not reassigned here: it is set when the row is reserved, and overwriting it was GHSA-5mxw-g5mw-3v3w.
		updates["is_uploaded"] = true
	}
	if handler.tasks != nil && !asset.HasStorageMetadata() {
		if err := handler.tasks.PublishAssetObjectMetadata(c.Request.Context(), asset.ID); err != nil {
			handler.internalError(c, err)
			return
		}
	}
	err = handler.db.WithContext(c.Request.Context()).Model(&FileAsset{}).Where("id = ?", asset.ID).Updates(updates).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *Handler) issueAttachmentDestroy(c *gin.Context, user *auth.User) {
	asset, found, err := handler.attachmentByID(c.Request.Context(), c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "The required object does not exist."})
		return
	}
	// allow_permission([ADMIN], creator=True): a project admin may remove any attachment, and whoever uploaded one may remove their own.
	if !handler.requireAttachmentAdminOrCreator(c, user, asset) {
		return
	}
	now := handler.clock().UTC()
	// The row is flagged rather than removed, and the object is left in the bucket for the storage sweep to collect.
	err = handler.db.WithContext(c.Request.Context()).Model(&FileAsset{}).Where("id = ?", asset.ID).
		Updates(map[string]any{"is_deleted": true, "deleted_at": now, "updated_at": now}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	err = handler.publishIssueActivity(c, issueActivity{
		Type:    "attachment.activity.deleted",
		ActorID: user.ID, IssueID: c.Param("issue"), ProjectID: c.Param("id"),
		Notification: true, Origin: handler.origin(), Epoch: now,
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// attachmentByID reads the row scoped to the whole URL, so an id belonging to another issue or project cannot be reached.
func (handler *Handler) attachmentByID(ctx context.Context, c *gin.Context) (FileAsset, bool, error) {
	var asset FileAsset
	err := handler.db.WithContext(ctx).Table("file_assets fa").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("fa.id = ? AND fa.issue_id = ? AND fa.project_id = ? AND w.slug = ? AND fa.deleted_at IS NULL",
			c.Param("asset"), c.Param("issue"), c.Param("id"), c.Param("slug")).
		Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FileAsset{}, false, nil
	}
	if err != nil {
		return FileAsset{}, false, err
	}
	return asset, true, nil
}

// attachmentJSON is IssueAttachmentSerializer, which is fields = "__all__" plus the asset_url property.
func attachmentJSON(asset FileAsset, slug string) gin.H {
	return gin.H{
		"id": asset.ID, "created_at": asset.CreatedAt, "updated_at": asset.UpdatedAt,
		"deleted_at": asset.DeletedAt, "attributes": decodeJSON(asset.Attributes),
		"asset": asset.Asset, "entity_type": asset.EntityType, "entity_identifier": asset.EntityIdentifier,
		"is_deleted": asset.IsDeleted, "is_archived": asset.IsArchived,
		"external_id": asset.ExternalID, "external_source": asset.ExternalSource,
		"size": asset.Size, "is_uploaded": asset.IsUploaded,
		"storage_metadata": decodeJSON(asset.StorageMetadata),
		"created_by":       asset.CreatedByID, "updated_by": asset.UpdatedByID,
		"user": asset.UserID, "workspace": asset.WorkspaceID, "draft_issue": asset.DraftIssueID,
		"project": asset.ProjectID, "issue": asset.IssueID, "comment": asset.CommentID, "page": asset.PageID,
		"asset_url": attachmentURL(asset, slug),
	}
}

// attachmentURL is FileAsset.asset_url for the attachment entity type: the route that redirects to a signed download.
func attachmentURL(asset FileAsset, slug string) string {
	return "/api/assets/v2/workspaces/" + slug + "/projects/" + deref(asset.ProjectID) +
		"/issues/" + deref(asset.IssueID) + "/attachments/" + asset.ID + "/"
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// attachmentName is the name the upload recorded, which becomes the filename in the download's content disposition.
func attachmentName(asset FileAsset) string {
	var attributes struct {
		Name string `json:"name"`
	}
	if len(asset.Attributes) == 0 {
		return ""
	}
	if json.Unmarshal(asset.Attributes, &attributes) != nil {
		return ""
	}
	return attributes.Name
}

// parseAttachmentSize accepts what int() accepts of a JSON size: a number or a string holding one. Django's int() raises on anything else, which BaseAPIView turns into a 400.
func parseAttachmentSize(raw json.RawMessage) (int64, bool) {
	text := strings.TrimSpace(string(raw))
	if unquoted, err := strconv.Unquote(text); err == nil {
		text = strings.TrimSpace(unquoted)
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		// int() also accepts a float string only through a cast, not directly, so a fractional size is rejected the same way Django rejects it.
		return 0, false
	}
	return value, true
}

// fileSizeLimit is settings.FILE_SIZE_LIMIT, five megabytes when nothing configured one.
func (handler *Handler) fileSizeLimit() int64 {
	if handler.settings.FileSizeLimit > 0 {
		return handler.settings.FileSizeLimit
	}
	return 5242880
}

// requireAttachmentAdminOrCreator is allow_permission([ADMIN], creator=True, model=FileAsset): whoever uploaded an attachment may remove it, and otherwise a project or workspace admin may.
func (handler *Handler) requireAttachmentAdminOrCreator(c *gin.Context, user *auth.User, asset FileAsset) bool {
	member, found, err := handler.activeProjectMember(c.Request.Context(), c.Param("slug"), c.Param("id"), user.ID)
	if err != nil {
		handler.internalError(c, err)
		return false
	}
	if found {
		// The creator branch runs before the role check, the same way it does for an issue.
		if asset.CreatedByID != nil && *asset.CreatedByID == user.ID {
			return true
		}
		if member.Role == roleAdmin {
			return true
		}
		workspaceRole, _, err := handler.workspaceMemberRole(c.Request.Context(), c.Param("slug"), user.ID)
		if err != nil {
			handler.internalError(c, err)
			return false
		}
		if workspaceRole == roleAdmin {
			return true
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "You don't have the required permissions."})
	return false
}

// marshalUnescaped encodes without Go's HTML escaping, so a stored attribute reads the way Django stores it; jsonb would normalise either form to the same value, but the bytes that reach the column should match.
func marshalUnescaped(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// randomHex is uuid4().hex in the object key, which only has to be unguessable and unique.
func randomHex(byteLength int) (string, error) {
	buffer := make([]byte, byteLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
