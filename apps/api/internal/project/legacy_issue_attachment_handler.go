package project

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
)

// The v1 attachment routes, which upload through the API rather than handing the browser a signed policy.
//
// They differ from their v2 neighbours in more than the transport. The list here shows every attachment rather than only the ones that finished uploading, and the delete really removes the row rather than flagging it — and takes the object out of the bucket on the way.
func (handler *Handler) registerLegacyIssueAttachmentRoutes(router gin.IRouter) {
	const collection = "/api/workspaces/:slug/projects/:id/issues/:issue/issue-attachments/"
	router.GET(collection, handler.authenticatedIssueUUID(handler.legacyAttachmentList))
	router.POST(collection, handler.authenticatedIssueUUID(handler.legacyAttachmentCreate))
	router.DELETE(collection+":asset/", handler.authenticatedIssueUUID(handler.legacyAttachmentDestroy))
}

// legacyAttachmentList is every attachment on the work item, finished or not.
//
// The v2 list narrows to is_uploaded; this one does not, so a row whose upload was started and never completed shows here and not there.
func (handler *Handler) legacyAttachmentList(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")
	var assets []FileAsset
	err := handler.db.WithContext(c.Request.Context()).Table("file_assets fa").
		Joins("JOIN workspaces w ON w.id = fa.workspace_id").
		Where("fa.issue_id = ? AND fa.project_id = ? AND w.slug = ? AND fa.deleted_at IS NULL",
			issueID, projectID, slug).
		Order("fa.created_at DESC").Find(&assets).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	results := make([]gin.H, 0, len(assets))
	for _, asset := range assets {
		results = append(results, attachmentJSON(asset, slug))
	}
	drf.Respond(c, http.StatusOK, results)
}

// legacyAttachmentCreate takes the bytes and files them against the work item.
func (handler *Handler) legacyAttachmentCreate(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember, roleGuest) {
		return
	}
	slug, projectID, issueID := c.Param("slug"), c.Param("id"), c.Param("issue")

	var workspaceIDs []string
	err := handler.db.WithContext(c.Request.Context()).Table("workspaces").
		Where("slug = ? AND deleted_at IS NULL", slug).Limit(1).Pluck("id", &workspaceIDs).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if len(workspaceIDs) == 0 {
		handler.notFound(c)
		return
	}

	file, header, err := c.Request.FormFile("asset")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"asset": []string{"No file was submitted."}})
		return
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, handler.settings.FileSizeLimit+1))
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if handler.settings.FileSizeLimit > 0 && int64(len(payload)) > handler.settings.FileSizeLimit {
		c.JSON(http.StatusBadRequest, gin.H{"asset": []string{"File too large. Size should not exceed 5 MB."}})
		return
	}
	if handler.storage == nil {
		handler.internalError(c, errors.New("object storage is not configured"))
		return
	}

	key := legacyUploadPath(&workspaceIDs[0], header.Filename)
	if err := handler.storage.PutObject(c.Request.Context(), key, headerContentType(header), payload, false); err != nil {
		handler.internalError(c, err)
		return
	}

	now := handler.clock().UTC()
	assetID := uuid.NewString()
	err = handler.db.WithContext(c.Request.Context()).Table("file_assets").Create(map[string]any{
		"id": assetID, "created_at": now, "updated_at": now,
		"created_by_id": user.ID, "updated_by_id": nil,
		"asset": key, "size": len(payload), "attributes": string(legacyAttributes(c)),
		"is_deleted": false, "is_archived": false,
		// The row is marked uploaded on the way in, because the bytes are already there — where a v2 row waits for the browser to say so.
		"is_uploaded":  true,
		"workspace_id": workspaceIDs[0], "project_id": projectID, "issue_id": issueID,
		"entity_type": attachmentEntityType,
	}).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}

	var stored FileAsset
	if err := handler.db.WithContext(c.Request.Context()).Table("file_assets").Where("id = ?", assetID).Take(&stored).Error; err != nil {
		handler.internalError(c, err)
		return
	}
	serialized := attachmentJSON(stored, slug)
	current, err := json.Marshal(serialized)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	currentInstance := string(current)
	err = handler.publishIssueActivity(c, issueActivity{
		Type:    "attachment.activity.created",
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		CurrentInstance: &currentInstance,
		Notification:    true, Origin: handler.origin(), Epoch: now,
	})
	if err != nil {
		handler.internalError(c, err)
		return
	}
	drf.Respond(c, http.StatusCreated, serialized)
}

// legacyAttachmentDestroy really removes the row and the object.
//
// This is the one attachment path that hard-deletes. Its v2 neighbour flags the row and leaves the object for the storage sweep; this one takes both away at once, so a mistaken delete here cannot be undone.
func (handler *Handler) legacyAttachmentDestroy(c *gin.Context, user *auth.User) {
	asset, found, err := handler.attachmentByID(c.Request.Context(), c)
	if err != nil {
		handler.internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Issue attachment not found."})
		return
	}
	if !handler.requireAttachmentAdminOrCreator(c, user, asset) {
		return
	}
	if handler.storage != nil {
		// The object goes first, and a bucket that refuses is not allowed to stop the row from going.
		_ = handler.storage.RemoveObject(c.Request.Context(), asset.Asset)
	}
	err = handler.db.WithContext(c.Request.Context()).Exec("DELETE FROM file_assets WHERE id = ?", asset.ID).Error
	if err != nil {
		handler.internalError(c, err)
		return
	}
	now := handler.clock().UTC()
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
