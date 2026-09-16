// Package issues holds what Issue.save does, which is the part of a work item neither API writes for itself.
//
// Both APIs create and update work items through a serializer, and a serializer's save() ends in the model's. That is where the sequence number is handed out under a lock, where the sort order is derived from the work items already in the chosen state, where a missing state is filled in, where completed_at follows the state, and where the plain-text copy of the description is made. None of it is reachable through a serializer field, and all of it has to happen the same way on both sides while the two run together — a sequence number the two halves disagree about is two work items with the same key.
package issues

import (
	"crypto/sha256"
	"encoding/binary"
	"regexp"
	"time"

	"gorm.io/gorm"
)

// The column defaults the model declares, which apply whenever a serializer leaves the field out.
const (
	defaultDescriptionHTML = "<p></p>"
	defaultSortOrder       = 65535.0
	// sortOrderStep is what a new work item is placed past the last one in its state.
	sortOrderStep = 10000.0
)

// AdvisoryLockKey is convert_uuid_to_integer: the first eight bytes of the project id's SHA-256, read as a signed big-endian integer.
//
// Both implementations have to agree on it exactly. During the transition a create can arrive at either side, and a lock they compute differently is no lock at all — two work items would take the same sequence number.
func AdvisoryLockKey(projectID string) int64 {
	digest := sha256.Sum256([]byte(projectID))
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

// PrepareCreate is Issue.save's adding path over the values a serializer produced, and answers with the sequence number it handed out.
//
// The project is locked first. The sequence number and the sort order are both derived from rows another request could be writing at the same moment, so without the lock two work items created together could claim the same number.

// CreateDefaults is what a work item gets for the NOT NULL columns a request does not have to send.
//
// Django fills each of these from the field's own default, so a caller that leaves one out still writes a row; a map handed straight to the database does not, and the insert fails on the constraint. "none" rather than an empty string is the priority for having none — db.0043 is where every null one was turned into that word.
//
// It is a named table rather than a run of ifs so that TestCreateDefaultsCoverTheRequiredColumns can check it against the schema: every NOT NULL column of issues with no database default is either set above from the request or defaulted here.
var CreateDefaults = map[string]any{
	"priority":         "none",
	"description_json": []byte("{}"),
	"is_draft":         false,
}

// CreateAssigned is the rest: the NOT NULL columns this function sets itself, listed so the same test can tell "covered" from "forgotten".
var CreateAssigned = []string{
	"id", "name", "created_at", "updated_at", "project_id", "workspace_id",
	"sequence_id", "sort_order", "description_html",
}

func PrepareCreate(tx *gorm.DB, values map[string]any, projectID, workspaceID, actorID string, now time.Time) (int64, error) {
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", AdvisoryLockKey(projectID)).Error; err != nil {
		return 0, err
	}

	stateID, ok := values["state_id"].(string)
	if !ok || stateID == "" {
		// A work item with no state of its own takes the project's default, and failing that whatever state comes first.
		resolved, err := DefaultStateID(tx, projectID)
		if err != nil {
			return 0, err
		}
		stateID = resolved
	}

	sequence, err := NextSequence(tx, projectID)
	if err != nil {
		return 0, err
	}
	sortOrder, err := NextSortOrder(tx, projectID, stateID)
	if err != nil {
		return 0, err
	}

	values["created_at"] = now
	values["updated_at"] = now
	values["created_by_id"] = actorID
	values["updated_by_id"] = actorID
	values["project_id"] = projectID
	// ProjectBaseModel.save reads the workspace off the project rather than taking it from the request.
	values["workspace_id"] = workspaceID
	values["state_id"] = stateID
	values["sequence_id"] = sequence
	if _, given := values["sort_order"]; !given {
		values["sort_order"] = sortOrder
	}
	if _, given := values["description_html"]; !given {
		values["description_html"] = defaultDescriptionHTML
	}
	values["description_stripped"] = StripTags(stringOrEmpty(values["description_html"]))
	for column, fallback := range CreateDefaults {
		if _, given := values[column]; !given {
			values[column] = fallback
		}
	}

	// completed_at is set on creation when the chosen state is a completed one, which is what _sync_completed_at does while adding.
	group, err := StateGroup(tx, stateID)
	if err != nil {
		return 0, err
	}
	if group == "completed" {
		values["completed_at"] = now
	}
	return sequence, nil
}

// ApplyUpdate is Issue.save's non-adding path, which runs whenever a serializer saves a work item and writes three columns the request never mentions.
//
// The state is filled in when it is missing, so clearing a work item's state does not leave it without one — it lands on the project's default instead. completed_at follows the state whenever the state really changes: into a completed group it becomes the moment of the change, out of one it becomes null. And description_stripped is recomputed from whatever html the work item ends up with, so the plain-text copy the search reads cannot fall behind the rich text.
func ApplyUpdate(tx *gorm.DB, values map[string]any, projectID string, currentStateID *string, currentHTML string, now time.Time) error {
	if err := ensureState(tx, values, projectID); err != nil {
		return err
	}
	if err := syncCompletedAt(tx, values, currentStateID, now); err != nil {
		return err
	}
	html := currentHTML
	if value, given := values["description_html"]; given {
		html = stringOrEmpty(value)
	}
	values["description_stripped"] = StripTags(html)
	return nil
}

// ensureState is _ensure_default_state on the update path. It runs on every save rather than only on creation, so a request that sets state_id to null does not clear the state — it moves the work item onto the default.
func ensureState(tx *gorm.DB, values map[string]any, projectID string) error {
	value, given := values["state_id"]
	if !given {
		return nil
	}
	if identifier, ok := value.(string); ok && identifier != "" {
		return nil
	}
	resolved, err := DefaultStateID(tx, projectID)
	if err != nil {
		return err
	}
	if resolved == "" {
		// A project with no state at all leaves the work item without one, which is what `default_state or first` does when both are missing.
		return nil
	}
	values["state_id"] = resolved
	return nil
}

// syncCompletedAt is _sync_completed_at on the update path. It fires only when the state really changes, so saving a work item with the state it already had leaves the timestamp alone — which is what keeps the moment a work item was finished from being rewritten by every later edit.
func syncCompletedAt(tx *gorm.DB, values map[string]any, currentStateID *string, now time.Time) error {
	value, given := values["state_id"]
	if !given {
		return nil
	}
	stateID, ok := value.(string)
	if !ok || stateID == "" {
		// With no state there is nothing to follow, and the timestamp is left as it stands.
		return nil
	}
	if currentStateID != nil && *currentStateID == stateID {
		return nil
	}
	group, err := StateGroup(tx, stateID)
	if err != nil {
		return err
	}
	if group == "completed" {
		values["completed_at"] = now
		return nil
	}
	values["completed_at"] = nil
	return nil
}

// NextSequence is the number the new work item takes, which counts from the sequences already handed out rather than from the work items that still exist — so deleting a work item does not free its key.
func NextSequence(tx *gorm.DB, projectID string) (int64, error) {
	var largest []int64
	err := tx.Table("issue_sequences").Where("project_id = ?", projectID).
		Select("COALESCE(MAX(sequence), 0)").Scan(&largest).Error
	if err != nil {
		return 0, err
	}
	if len(largest) > 0 && largest[0] > 0 {
		return largest[0] + 1, nil
	}
	return 1, nil
}

// NextSortOrder puts a new work item after everything already in its state, and leaves it at the default when the state is empty.
func NextSortOrder(tx *gorm.DB, projectID, stateID string) (float64, error) {
	var largest []float64
	err := tx.Table("issues").Where("project_id = ? AND state_id = ? AND deleted_at IS NULL", projectID, stateID).
		Select("COALESCE(MAX(sort_order), -1)").Scan(&largest).Error
	if err != nil {
		return 0, err
	}
	if len(largest) > 0 && largest[0] >= 0 {
		return largest[0] + sortOrderStep, nil
	}
	return defaultSortOrder, nil
}

// DefaultStateID is _ensure_default_state: the project's default state, and failing that whatever non-triage state comes first.
func DefaultStateID(tx *gorm.DB, projectID string) (string, error) {
	var identifiers []string
	err := tx.Table("states").
		Where(`project_id = ? AND deleted_at IS NULL AND is_triage = FALSE`, projectID).
		Order(`"default" DESC, sequence ASC`).Limit(1).Pluck("id", &identifiers).Error
	if err != nil {
		return "", err
	}
	if len(identifiers) == 0 {
		return "", nil
	}
	return identifiers[0], nil
}

// StateGroup reads the group a state belongs to, which is what decides whether a work item counts as finished.
func StateGroup(tx *gorm.DB, stateID string) (string, error) {
	if stateID == "" {
		return "", nil
	}
	var groups []string
	if err := tx.Table("states").Where("id = ?", stateID).Pluck(`"group"`, &groups).Error; err != nil {
		return "", err
	}
	if len(groups) == 0 {
		return "", nil
	}
	return groups[0], nil
}

var tagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

// StripTags is Django's strip_tags over the description html, which is null when there is no html rather than an empty string.
func StripTags(html string) *string {
	if html == "" {
		return nil
	}
	stripped := tagPattern.ReplaceAllString(html, "")
	return &stripped
}

func stringOrEmpty(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
