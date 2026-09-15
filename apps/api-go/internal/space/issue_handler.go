package space

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

func (handler *Handler) registerIssueRoutes(router gin.IRouter) {
	router.GET("/api/public/anchor/:anchor/issues/:issue/", handler.issueRetrieve)
}

// issueRetrieve returns one work item of a published board, with its votes and its reactions folded in.
//
// The board is looked up **without** asking what it is published as, and the work item is read through the manager the board itself reads — so a draft, an archived one or one in triage is not here. A work item that is not found is answered with a **null body** and a 200 rather than a 404, because the view serializes whatever the query returned.
func (handler *Handler) issueRetrieve(c *gin.Context) {
	board, found, err := handler.boardByAnchor(c)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if !found {
		notFound(c)
		return
	}
	var rows []spaceIssueRow
	err = handler.db.WithContext(c.Request.Context()).Table("issues i").
		Select(spaceIssueSelection()).
		Joins("JOIN projects p ON p.id = i.project_id").
		Joins("LEFT JOIN states s ON s.id = i.state_id").
		Where("i.id = ? AND i.workspace_id = ? AND i.project_id = ?",
			c.Param("issue"), board.WorkspaceID, board.ProjectID).
		Where(`i.deleted_at IS NULL AND s.group IS DISTINCT FROM 'triage'
			AND i.archived_at IS NULL AND p.archived_at IS NULL AND i.is_draft = FALSE`).
		Limit(1).Scan(&rows).Error
	if err != nil {
		handler.serverError(c, err)
		return
	}
	if len(rows) == 0 {
		// The serializer is handed nothing and answers nothing, which is a null body rather than a refusal.
		drf.Respond(c, http.StatusOK, nil)
		return
	}
	votes, err := handler.issueVoteItems(c, rows[0].ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	reactions, err := handler.issueReactionItems(c, rows[0].ID)
	if err != nil {
		handler.serverError(c, err)
		return
	}
	drf.Respond(c, http.StatusOK, spaceIssueJSON(rows[0], votes, reactions))
}

// spaceIssueRow is the values() projection the board reads.
type spaceIssueRow struct {
	ID                  string         `gorm:"column:id"`
	Name                string         `gorm:"column:name"`
	StateID             *string        `gorm:"column:state_id"`
	SortOrder           float64        `gorm:"column:sort_order"`
	DescriptionJSON     []byte         `gorm:"column:description_json"`
	DescriptionHTML     string         `gorm:"column:description_html"`
	DescriptionStripped *string        `gorm:"column:description_stripped"`
	DescriptionBinary   []byte         `gorm:"column:description_binary"`
	ModuleIDs           pq.StringArray `gorm:"column:module_ids;type:uuid[]"`
	LabelIDs            pq.StringArray `gorm:"column:label_ids;type:uuid[]"`
	AssigneeIDs         pq.StringArray `gorm:"column:assignee_ids;type:uuid[]"`
	EstimatePoint       *string        `gorm:"column:estimate_point_id"`
	Priority            string         `gorm:"column:priority"`
	StartDate           *time.Time     `gorm:"column:start_date"`
	TargetDate          *time.Time     `gorm:"column:target_date"`
	SequenceID          int64          `gorm:"column:sequence_id"`
	ProjectID           string         `gorm:"column:project_id"`
	ParentID            *string        `gorm:"column:parent_id"`
	CycleID             *string        `gorm:"column:cycle_id"`
	CreatedByID         *string        `gorm:"column:created_by_id"`
	StateGroup          *string        `gorm:"column:state_group"`
}

// spaceIssueSelection is the columns the projection names plus the three id lists it aggregates.
//
// The three lists carry the same filters the application's own detail does: a label link and an assignee link have to be live, an assignee has to still be an active member of the project, and a module has to be unarchived.
func spaceIssueSelection() string {
	return `i.id, i.name, i.state_id, i.sort_order, i.description_json, i.description_html,
		i.description_stripped, i.description_binary, i.estimate_point_id, i.priority,
		i.start_date, i.target_date, i.sequence_id, i.project_id, i.parent_id, i.created_by_id,
		(SELECT s2.group FROM states s2 WHERE s2.id = i.state_id) AS state_group,
		(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id AND ci.deleted_at IS NULL LIMIT 1) AS cycle_id,
		COALESCE((SELECT ARRAY_AGG(DISTINCT il.label_id) FROM issue_labels il
			WHERE il.issue_id = i.id AND il.deleted_at IS NULL), '{}') AS label_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT ia.assignee_id) FROM issue_assignees ia
			JOIN project_members pm ON pm.member_id = ia.assignee_id AND pm.project_id = i.project_id AND pm.is_active = TRUE
			WHERE ia.issue_id = i.id AND ia.deleted_at IS NULL), '{}') AS assignee_ids,
		COALESCE((SELECT ARRAY_AGG(DISTINCT mi.module_id) FROM module_issues mi
			JOIN modules m ON m.id = mi.module_id AND m.archived_at IS NULL
			WHERE mi.issue_id = i.id AND mi.deleted_at IS NULL), '{}') AS module_ids`
}

// issueVoteItems reads the votes with the voter folded into each one.
func (handler *Handler) issueVoteItems(c *gin.Context, issueID string) ([]gin.H, error) {
	var rows []struct {
		Vote        int     `gorm:"column:vote"`
		ActorID     string  `gorm:"column:actor_id"`
		FirstName   string  `gorm:"column:first_name"`
		LastName    string  `gorm:"column:last_name"`
		Avatar      string  `gorm:"column:avatar"`
		AvatarAsset *string `gorm:"column:avatar_asset_id"`
		DisplayName string  `gorm:"column:display_name"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("issue_votes v").
		Select("v.vote, v.actor_id, u.first_name, u.last_name, u.avatar, u.avatar_asset_id, u.display_name").
		Joins("JOIN users u ON u.id = v.actor_id").
		Where("v.issue_id = ? AND v.deleted_at IS NULL", issueID).
		Order("v.created_at").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, gin.H{
			"vote": row.Vote,
			"actor_details": gin.H{
				"id": row.ActorID, "first_name": row.FirstName, "last_name": row.LastName,
				"avatar": row.Avatar, "avatar_url": staticAvatarURL(row.AvatarAsset, row.Avatar),
				"display_name": row.DisplayName,
			},
		})
	}
	return items, nil
}

// issueReactionItems reads the reactions the same way, with one difference that is not deliberate.
//
// Each reaction's `avatar_url` is computed from the **voter's** avatar rather than the reactor's — the expression names the votes relation where it means the reactions one — so a work item with reactions and no votes reports a null url for every reaction, and one with both reports whichever voter the join lands on. Reproduced rather than corrected: the avatar beside it is the reactor's, and a client that reads either one sees what Django shows it.
func (handler *Handler) issueReactionItems(c *gin.Context, issueID string) ([]gin.H, error) {
	var rows []struct {
		Reaction    string  `gorm:"column:reaction"`
		ActorID     string  `gorm:"column:actor_id"`
		FirstName   string  `gorm:"column:first_name"`
		LastName    string  `gorm:"column:last_name"`
		Avatar      string  `gorm:"column:avatar"`
		DisplayName string  `gorm:"column:display_name"`
		VoterAsset  *string `gorm:"column:voter_asset"`
		VoterAvatar *string `gorm:"column:voter_avatar"`
	}
	err := handler.db.WithContext(c.Request.Context()).Table("issue_reactions r").
		Select(`r.reaction, r.actor_id, u.first_name, u.last_name, u.avatar, u.display_name,
			(SELECT vu.avatar_asset_id FROM issue_votes v JOIN users vu ON vu.id = v.actor_id
				WHERE v.issue_id = r.issue_id AND v.deleted_at IS NULL LIMIT 1) AS voter_asset,
			(SELECT vu.avatar FROM issue_votes v JOIN users vu ON vu.id = v.actor_id
				WHERE v.issue_id = r.issue_id AND v.deleted_at IS NULL LIMIT 1) AS voter_avatar`).
		Joins("JOIN users u ON u.id = r.actor_id").
		Where("r.issue_id = ? AND r.deleted_at IS NULL", issueID).
		Order("r.created_at").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		var avatarURL any
		switch {
		case row.VoterAsset != nil:
			avatarURL = "/api/assets/v2/static/" + *row.VoterAsset + "/"
		case row.VoterAvatar != nil && *row.VoterAvatar != "":
			avatarURL = *row.VoterAvatar
		}
		items = append(items, gin.H{
			"reaction": row.Reaction,
			"actor_details": gin.H{
				"id": row.ActorID, "first_name": row.FirstName, "last_name": row.LastName,
				"avatar": row.Avatar, "avatar_url": avatarURL,
				"display_name": row.DisplayName,
			},
		})
	}
	return items, nil
}

func staticAvatarURL(asset *string, avatar string) any {
	if asset != nil {
		return "/api/assets/v2/static/" + *asset + "/"
	}
	if avatar != "" {
		return avatar
	}
	return nil
}

// spaceIssueJSON is the values() projection: twenty-three keys, with the votes and the reactions beside them.
func spaceIssueJSON(row spaceIssueRow, votes, reactions []gin.H) gin.H {
	return gin.H{
		"id": row.ID, "name": row.Name, "state_id": row.StateID, "sort_order": row.SortOrder,
		"description_json": decodeJSON(row.DescriptionJSON), "description_html": row.DescriptionHTML,
		"description_stripped": row.DescriptionStripped, "description_binary": row.DescriptionBinary,
		"module_ids": stringsOrEmpty(row.ModuleIDs), "label_ids": stringsOrEmpty(row.LabelIDs),
		"assignee_ids": stringsOrEmpty(row.AssigneeIDs), "estimate_point": row.EstimatePoint,
		"priority": row.Priority, "start_date": dayOrNil(row.StartDate), "target_date": dayOrNil(row.TargetDate),
		"sequence_id": row.SequenceID, "project_id": row.ProjectID, "parent_id": row.ParentID,
		"cycle_id": row.CycleID, "created_by": row.CreatedByID, "state__group": row.StateGroup,
		"vote_items": votes, "reaction_items": reactions,
	}
}

func stringsOrEmpty(values pq.StringArray) []string {
	if values == nil {
		return []string{}
	}
	return values
}
