package project

import (
	"time"

	"gorm.io/gorm"
)

// applyIssueSavePath is Issue.save's non-adding path, which runs whenever a serializer saves a work item and changes columns the request never mentioned.
//
// Three of them. The state is filled in when it is missing, so clearing a work item's state does not leave it without one — it lands on the project's default instead. completed_at follows the state whenever the state really changes: into a completed group it becomes the moment of the change, out of one it becomes null. And description_stripped is recomputed from whatever html the work item ends up with, so the plain-text copy the search reads cannot fall behind the rich text.
//
// None of the three is reachable through the serializer, which is why an update that never mentions them still has to write them.
func applyIssueSavePath(tx *gorm.DB, issue Issue, updates map[string]any, now time.Time) error {
	if err := ensureIssueState(tx, issue, updates); err != nil {
		return err
	}
	if err := syncIssueCompletedAt(tx, issue, updates, now); err != nil {
		return err
	}
	html := issue.DescriptionHTML
	if value, given := updates["description_html"]; given {
		html = stringOrEmpty(value)
	}
	updates["description_stripped"] = strippedIssueDescription(html)
	return nil
}

// ensureIssueState is _ensure_default_state on the update path: a work item left without a state takes the project's default, and failing that whatever non-triage state comes first.
//
// It runs on every save rather than only on creation, so a request that sets state_id to null does not clear the state — it moves the work item onto the default.
func ensureIssueState(tx *gorm.DB, issue Issue, updates map[string]any) error {
	value, given := updates["state_id"]
	if !given {
		return nil
	}
	if identifier, ok := value.(string); ok && identifier != "" {
		return nil
	}
	resolved, err := defaultStateFor(tx, issue.ProjectID)
	if err != nil {
		return err
	}
	if resolved == "" {
		// A project with no state at all leaves the work item without one, which is what `default_state or first` does when both are missing.
		return nil
	}
	updates["state_id"] = resolved
	return nil
}

// syncIssueCompletedAt is _sync_completed_at on the update path. It fires only when the state really changes, so saving a work item with the state it already had leaves the timestamp alone — which is what keeps the moment a work item was finished from being rewritten by every later edit.
func syncIssueCompletedAt(tx *gorm.DB, issue Issue, updates map[string]any, now time.Time) error {
	value, given := updates["state_id"]
	if !given {
		return nil
	}
	stateID, ok := value.(string)
	if !ok || stateID == "" {
		// With no state there is nothing to follow, and the timestamp is left as it stands.
		return nil
	}
	if issue.StateID != nil && *issue.StateID == stateID {
		return nil
	}
	group, err := stateGroupOf(tx, stateID)
	if err != nil {
		return err
	}
	if group == "completed" {
		updates["completed_at"] = now
		return nil
	}
	updates["completed_at"] = nil
	return nil
}
