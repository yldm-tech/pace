package project

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/drf"
	"gorm.io/gorm"
)

func (handler *Handler) registerIssueDatesRoutes(router gin.IRouter) {
	router.POST("/api/workspaces/:slug/projects/:id/issue-dates/", handler.authenticated(handler.issueBulkUpdateDates))
}

// issueBulkUpdateDates moves the start and target dates of several issues at once, which is what dragging a bar in the timeline view does.
func (handler *Handler) issueBulkUpdateDates(c *gin.Context, user *auth.User) {
	if !handler.requireProjectRole(c, user, roleAdmin, roleMember) {
		return
	}
	slug, projectID := c.Param("slug"), c.Param("id")
	var request struct {
		Updates []struct {
			ID         string          `json:"id"`
			StartDate  json.RawMessage `json:"start_date"`
			TargetDate json.RawMessage `json:"target_date"`
		} `json:"updates"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.invalidDetail(c)
		return
	}

	identifiers := make([]string, 0, len(request.Updates))
	for _, update := range request.Updates {
		identifiers = append(identifiers, update.ID)
	}
	// The plain soft-delete manager, so an archived or draft issue can have its dates moved too.
	issues := map[string]Issue{}
	if len(identifiers) > 0 {
		var rows []Issue
		err := handler.db.WithContext(c.Request.Context()).Table("issues i").
			Joins("JOIN workspaces w ON w.id = i.workspace_id").
			Where("i.id IN ? AND i.project_id = ? AND w.slug = ? AND i.deleted_at IS NULL",
				identifiers, projectID, slug).Find(&rows).Error
		if err != nil {
			handler.internalError(c, err)
			return
		}
		for _, row := range rows {
			issues[row.ID] = row
		}
	}

	now := handler.clock().UTC()
	type dateChange struct {
		issueID    string
		startDate  *string
		targetDate *string
	}
	changes := make([]dateChange, 0, len(request.Updates))
	for _, update := range request.Updates {
		issue, known := issues[update.ID]
		if !known {
			// An id that names nothing in this project is skipped rather than refused.
			continue
		}
		start, startGiven := dateFromRaw(update.StartDate)
		target, targetGiven := dateFromRaw(update.TargetDate)

		// The pair is validated against whichever half the request did not supply, so moving one end cannot cross the other.
		effectiveStart, startOK := dateOrExisting(start, startGiven, issue.StartDate)
		effectiveTarget, targetOK := dateOrExisting(target, targetGiven, issue.TargetDate)
		if !startOK || !targetOK {
			// Django parses the submitted string with strptime, which raises on a malformed date and answers 500.
			handler.internalError(c, errMalformedDate)
			return
		}
		if effectiveStart != nil && effectiveTarget != nil && effectiveStart.After(*effectiveTarget) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Start date cannot exceed target date"})
			return
		}

		change := dateChange{issueID: update.ID}
		// A falsy value is not a change: Django tests the parsed value for truth, so an explicit null or an empty string leaves the column alone rather than clearing it.
		if startGiven && start != "" {
			change.startDate = &start
			if err := handler.publishDateActivity(c, user, update.ID, projectID, "start_date", start, issue.StartDate, now); err != nil {
				handler.internalError(c, err)
				return
			}
		}
		if targetGiven && target != "" {
			change.targetDate = &target
			if err := handler.publishDateActivity(c, user, update.ID, projectID, "target_date", target, issue.TargetDate, now); err != nil {
				handler.internalError(c, err)
				return
			}
		}
		if change.startDate != nil || change.targetDate != nil {
			changes = append(changes, change)
		}
	}

	if len(changes) > 0 {
		err := handler.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			for _, change := range changes {
				// bulk_update writes only the two date columns, so no timestamp moves.
				updates := map[string]any{}
				if change.startDate != nil {
					updates["start_date"] = *change.startDate
				}
				if change.targetDate != nil {
					updates["target_date"] = *change.targetDate
				}
				if err := tx.Model(&Issue{}).Where("id = ?", change.issueID).Updates(updates).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			handler.internalError(c, err)
			return
		}
	}
	drf.Respond(c, http.StatusOK, gin.H{"message": "Issues updated successfully"})
}

// publishDateActivity sends the activity one column change produces. current_instance carries the previous value stringified, which for a null column is the literal None.
func (handler *Handler) publishDateActivity(c *gin.Context, user *auth.User, issueID, projectID, column, value string, previous *time.Time, now time.Time) error {
	requested, err := json.Marshal(map[string]any{column: value})
	if err != nil {
		return err
	}
	current, err := json.Marshal(map[string]any{column: stringifyDate(previous)})
	if err != nil {
		return err
	}
	requestedData, currentInstance := string(requested), string(current)
	return handler.publishIssueActivity(c, issueActivity{
		Type: "issue.activity.updated", RequestedData: &requestedData, CurrentInstance: &currentInstance,
		ActorID: user.ID, IssueID: issueID, ProjectID: projectID,
		Origin: handler.origin(), Epoch: now,
	})
}

// dateFromRaw reads a date out of the request. A key that is absent, null or not a string is not a change.
func dateFromRaw(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

// errMalformedDate marks a date the caller sent that strptime could not read.
var errMalformedDate = errDate{}

type errDate struct{}

func (errDate) Error() string { return "issue dates: a submitted date is not in the expected format" }

// dateOrExisting is what the validation compares: the submitted value when there is one, and otherwise the column as it stands. The submitted value is parsed rather than compared as text, since a date has to be ordered as a date.
func dateOrExisting(value string, given bool, existing *time.Time) (*time.Time, bool) {
	if given && value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return nil, false
		}
		return &parsed, true
	}
	return existing, true
}

// stringifyDate is str() over a date column, which for a null one is the literal None.
func stringifyDate(value *time.Time) string {
	if value == nil {
		return "None"
	}
	return value.Format("2006-01-02")
}
