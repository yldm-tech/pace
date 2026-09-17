package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// namedState is a state as the history reports it: by name, and by the id the client links to.
type namedState struct {
	id   *string
	name *string
}

// stateNamed reads one state, and only when it belongs to the project — a state from elsewhere reads as no state rather than as an error.
func (tasks *IssueActivityTasks) stateNamed(ctx context.Context, stateID, projectID string) (namedState, error) {
	if stateID == "" {
		return namedState{}, nil
	}
	var rows []struct {
		ID   string `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	err := tasks.db.WithContext(ctx).Table("states").Select("id, name").
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", stateID, projectID).
		Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return namedState{}, err
	}
	return namedState{id: text(rows[0].ID), name: text(rows[0].Name)}, nil
}

// parentKey is a work item as its parent field reports it: the project's identifier and the number, which is what people call it.
type parentKey struct {
	id  *string
	key string
}

// issueKey reads the human-facing key of one work item. A work item that is gone reports an empty key rather than an error, which is what Django's first() leaves behind.
func (tasks *IssueActivityTasks) issueKey(ctx context.Context, issueID string) (parentKey, error) {
	if issueID == "" {
		return parentKey{}, nil
	}
	var rows []struct {
		ID         string `gorm:"column:id"`
		Identifier string `gorm:"column:identifier"`
		SequenceID int    `gorm:"column:sequence_id"`
	}
	err := tasks.db.WithContext(ctx).Table("issues i").
		Select("i.id, p.identifier, i.sequence_id").
		Joins("JOIN projects p ON p.id = i.project_id").
		Where("i.id = ?", issueID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return parentKey{}, err
	}
	return parentKey{id: text(rows[0].ID), key: fmt.Sprintf("%s-%d", rows[0].Identifier, rows[0].SequenceID)}, nil
}

// namedRow reads one column off one row, and says whether the row was there at all — which for a label or a person is the difference between a line of history and none.
func (tasks *IssueActivityTasks) namedRow(ctx context.Context, table, column, identifier string) (string, bool, error) {
	var values []string
	err := tasks.db.WithContext(ctx).Table(table).Where("id = ?", identifier).
		Limit(1).Pluck(column, &values).Error
	if err != nil || len(values) == 0 {
		return "", false, err
	}
	return values[0], true, nil
}

// estimateValue reads an estimate point's value and the kind of estimate it belongs to, which is what names the field the history writes.
func (tasks *IssueActivityTasks) estimateValue(ctx context.Context, pointID string) (*string, string, error) {
	if pointID == "" {
		return nil, "", nil
	}
	var rows []struct {
		Value string `gorm:"column:value"`
		Type  string `gorm:"column:type"`
	}
	err := tasks.db.WithContext(ctx).Table("estimate_points ep").
		Select("ep.value, e.type").
		Joins("JOIN estimates e ON e.id = ep.estimate_id").
		Where("ep.id = ? AND ep.deleted_at IS NULL", pointID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, "", err
	}
	return text(rows[0].Value), rows[0].Type, nil
}

// activityIDDifference is what a set of ids gained and what it lost.
//
// The two names are the session API's and the external one's for the same thing, and the first one present wins. Anything that is not a uuid is dropped rather than refused, which is what the validity check in each loop does.
func activityIDDifference(requested, current map[string]any, primary, fallback string) (added, dropped []string) {
	requestedIDs := activityIDSet(requested, primary, fallback)
	currentIDs := activityIDSet(current, primary, fallback)
	for identifier := range requestedIDs {
		if !currentIDs[identifier] && looksLikeUUID(identifier) {
			added = append(added, identifier)
		}
	}
	for identifier := range currentIDs {
		if !requestedIDs[identifier] && looksLikeUUID(identifier) {
			dropped = append(dropped, identifier)
		}
	}
	// Python walks a set, whose order is its own business. Sorting makes the history's order the same twice running, which is a difference with no weight and a great deal of convenience.
	sort.Strings(added)
	sort.Strings(dropped)
	return added, dropped
}

// activityIDSet is extract_ids: the first of the two keys that is present, whatever it holds.
func activityIDSet(snapshot map[string]any, primary, fallback string) map[string]bool {
	identifiers := map[string]bool{}
	if snapshot == nil {
		return identifiers
	}
	value, present := snapshot[primary]
	if !present {
		value = snapshot[fallback]
	}
	list, ok := value.([]any)
	if !ok {
		return identifiers
	}
	for _, item := range list {
		identifiers[activityTextOrEmpty(item)] = true
	}
	return identifiers
}

// firstPresent reads the first of two keys that carries something, which is how each tracker takes either name for the same field.
func firstPresent(snapshot map[string]any, primary, fallback string) string {
	if snapshot == nil {
		return ""
	}
	if value := activityTextOrEmpty(snapshot[primary]); value != "" {
		return value
	}
	return activityTextOrEmpty(snapshot[fallback])
}

// uuidOrNothing reads an id that is meant to be one, and reads anything else as nothing at all.
func uuidOrNothing(value string) string {
	if looksLikeUUID(value) {
		return value
	}
	return ""
}

// equalActivityValues compares two values the way Python compares the two json snapshots.
func equalActivityValues(left, right any) bool { return reflect.DeepEqual(left, right) }

// activityText renders a value for a history column, and keeps nothing as nothing.
func activityText(value any) *string {
	if value == nil {
		return nil
	}
	rendered := activityTextOrEmpty(value)
	return &rendered
}

// activityTextOrEmpty renders a value the way Python's str() would, and reads nothing as the empty string.
func activityTextOrEmpty(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

// truthy is Python's truthiness over a decoded json value, which is what the automation flag is read with.
func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case json.Number:
		return typed.String() != "0"
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	}
	return true
}

func text(value string) *string { return &value }

func textOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// anyString hands a task argument on in the shape the snapshot reader wants.
func anyString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// floatArgument reads the epoch, which Django sends as a number of seconds.
func floatArgument(arguments []any, keywords map[string]any, index int, name string) *float64 {
	value, ok := argument(arguments, keywords, index, name)
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case float64:
		return &typed
	case json.Number:
		if number, err := typed.Float64(); err == nil {
			return &number
		}
	case string:
		var number float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%g", &number); err == nil {
			return &number
		}
	}
	return nil
}

// boolArgumentWithDefault reads a flag that is true unless the caller said otherwise, which is what subscriber is.
func boolArgumentWithDefault(arguments []any, keywords map[string]any, index int, name string, fallback bool) bool {
	value, ok := argument(arguments, keywords, index, name)
	if !ok || value == nil {
		return fallback
	}
	return truthy(value)
}
