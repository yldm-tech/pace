package externalapi

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"github.com/yldm-tech/pace/apps/api/internal/htmlsanitizer"
)

// externalIssueInput is what the serializer makes of a work item payload: the columns to write, and the two sets that are written beside them.
type externalIssueInput struct {
	values map[string]any
	// The two many-to-many sets are kept apart because an absent one and an empty one mean different things: absent leaves the set alone, empty clears it.
	assignees    []string
	hasAssignees bool
	labels       []string
	hasLabels    bool
	// typeGiven marks a type the caller named, since a create without one falls back to the project's default type.
	typeGiven bool
}

// validateExternalIssue is IssueSerializer's to_internal_value followed by its validate.
//
// Every field is checked before any of them is rejected, so a payload with two bad fields answers with both — and validate() runs only when none of them failed, which is why a bad date never reports the date comparison as well.
//
// The checks that need a row read are not here: the state, the parent, the estimate point and the two sets are narrowed against the project by the caller, which has the database.
func validateExternalIssue(body map[string]json.RawMessage, partial bool) (externalIssueInput, gin.H) {
	input := externalIssueInput{values: map[string]any{}}
	errors := gin.H{}

	if raw, given := body["name"]; given {
		if value, failure := charField(raw, 255); failure != nil {
			errors["name"] = failure
		} else if *value == "" {
			errors["name"] = []string{"This field may not be blank."}
		} else {
			input.values["name"] = *value
		}
	} else if !partial {
		errors["name"] = []string{"This field is required."}
	}

	if raw, given := body["description_html"]; given {
		if value, failure := charField(raw, 0); failure != nil {
			errors["description_html"] = failure
		} else {
			input.values["description_html"] = *value
		}
	}
	if raw, given := body["priority"]; given {
		if value, failure := choiceField(raw, issuePriorityChoices); failure != nil {
			errors["priority"] = failure
		} else {
			input.values["priority"] = *value
		}
	}
	for _, field := range []struct{ name, column string }{
		{"start_date", "start_date"}, {"target_date", "target_date"}, {"archived_at", "archived_at"},
	} {
		raw, given := body[field.name]
		if !given {
			continue
		}
		value, failure := dateField(raw)
		if failure != nil {
			errors[field.name] = failure
			continue
		}
		if value == nil {
			input.values[field.column] = nil
			continue
		}
		input.values[field.column] = *value
	}
	if raw, given := body["deleted_at"]; given {
		if value, failure := dateTimeField(raw); failure != nil {
			errors["deleted_at"] = failure
		} else if value == nil {
			input.values["deleted_at"] = nil
		} else {
			input.values["deleted_at"] = *value
		}
	}
	if raw, given := body["point"]; given {
		if value, failure := integerField(raw, 0, 12); failure != nil {
			errors["point"] = failure
		} else if value == nil {
			input.values["point"] = nil
		} else {
			input.values["point"] = *value
		}
	}
	if raw, given := body["sequence_id"]; given {
		if value, failure := integerField(raw, 0, 0); failure != nil {
			errors["sequence_id"] = failure
		} else if value != nil {
			input.values["sequence_id"] = *value
		}
	}
	if raw, given := body["sort_order"]; given {
		if value, failure := floatField(raw); failure != nil {
			errors["sort_order"] = failure
		} else if value != nil {
			input.values["sort_order"] = *value
		}
	}
	if raw, given := body["is_draft"]; given {
		if value, failure := booleanField(raw); failure != nil {
			errors["is_draft"] = failure
		} else if value != nil {
			input.values["is_draft"] = *value
		}
	}
	for _, field := range []string{"external_source", "external_id"} {
		raw, given := body[field]
		if !given {
			continue
		}
		// Both external columns are nullable, so a null clears them rather than being refused the way a null name is.
		if string(raw) == "null" {
			input.values[field] = nil
			continue
		}
		value, failure := charField(raw, 255)
		if failure != nil {
			errors[field] = failure
			continue
		}
		input.values[field] = *value
	}

	// The relations are identified here and looked up by the caller. `type` and `type_id` are the same field under two names, so the one that comes later wins exactly as it does in Python.
	for _, relation := range []struct{ field, column string }{
		{"created_by", "created_by_id"}, {"parent", "parent_id"}, {"state", "state_id"},
		{"estimate_point", "estimate_point_id"}, {"type", "type_id"}, {"type_id", "type_id"},
	} {
		raw, given := body[relation.field]
		if !given {
			continue
		}
		value, failure := primaryKeyField(raw)
		if failure != nil {
			errors[relation.field] = failure
			continue
		}
		if value == nil {
			input.values[relation.column] = nil
		} else {
			input.values[relation.column] = *value
		}
		if relation.column == "type_id" {
			input.typeGiven = true
		}
	}

	for _, set := range []struct {
		field  string
		target *[]string
		marker *bool
	}{
		{"assignees", &input.assignees, &input.hasAssignees},
		{"labels", &input.labels, &input.hasLabels},
	} {
		raw, given := body[set.field]
		if !given {
			continue
		}
		values, failure := primaryKeyList(raw)
		if failure != nil {
			errors[set.field] = failure
			continue
		}
		*set.target = values
		*set.marker = true
	}

	if len(errors) > 0 {
		return externalIssueInput{}, errors
	}
	if failure := validateExternalIssueAcrossFields(&input); failure != nil {
		return externalIssueInput{}, failure
	}
	return input, nil
}

// validateExternalIssueAcrossFields is the serializer's validate(), which runs only once every field has passed. Each of its checks raises on its own, so at most one of them is ever reported.
func validateExternalIssueAcrossFields(input *externalIssueInput) gin.H {
	start, hasStart := input.values["start_date"].(time.Time)
	target, hasTarget := input.values["target_date"].(time.Time)
	if hasStart && hasTarget && start.After(target) {
		return gin.H{"non_field_errors": []string{"Start date cannot exceed target date"}}
	}
	html, given := input.values["description_html"].(string)
	if !given {
		return nil
	}
	// The description goes through libxml2 before the sanitizer, and markup that parses to nothing is refused rather than stored — which is how an empty description comes to be a bad request.
	rendered, err := htmlsanitizer.RoundTrip(html)
	if err != nil {
		return gin.H{"non_field_errors": []string{"Invalid HTML passed"}}
	}
	valid, _, cleaned := htmlsanitizer.ValidateHTMLContent(rendered)
	if !valid {
		return gin.H{"error": []string{"html content is not valid"}}
	}
	if cleaned != nil {
		rendered = *cleaned
	}
	input.values["description_html"] = rendered
	return nil
}

// issuePriorityChoices is the choice list the priority field carries, in the order the model declares it.
var issuePriorityChoices = []string{"urgent", "high", "medium", "low", "none"}

// charField is DRF's CharField: a string, or a number written as one. A boolean or a container is refused rather than printed.
func charField(raw json.RawMessage, limit int) (*string, []string) {
	if string(raw) == "null" {
		return nil, []string{"This field may not be null."}
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil, []string{"Not a valid string."}
	}
	var value string
	switch typed := decoded.(type) {
	case string:
		value = typed
	case float64:
		value = drfNumber(typed)
	default:
		return nil, []string{"Not a valid string."}
	}
	if limit > 0 && len([]rune(value)) > limit {
		return nil, []string{"Ensure this field has no more than " + strconv.Itoa(limit) + " characters."}
	}
	return &value, nil
}

// drfNumber writes a number the way Python's str() does, so a whole number carries no decimal point.
func drfNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func choiceField(raw json.RawMessage, choices []string) (*string, []string) {
	if string(raw) == "null" {
		return nil, []string{"This field may not be null."}
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil, []string{`"" is not a valid choice.`}
	}
	value, _ := decoded.(string)
	for _, choice := range choices {
		if value == choice {
			return &value, nil
		}
	}
	return nil, []string{fmt.Sprintf("%q is not a valid choice.", stringifyChoice(decoded))}
}

func stringifyChoice(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return drfNumber(typed)
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case nil:
		return "None"
	}
	return fmt.Sprint(value)
}

// dateField is DRF's DateField over ISO-8601, which is the day alone and nothing else — a datetime is refused rather than truncated.
func dateField(raw json.RawMessage) (*time.Time, []string) {
	if string(raw) == "null" {
		return nil, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil, []string{"Date has wrong format. Use one of these formats instead: YYYY-MM-DD."}
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, []string{"Date has wrong format. Use one of these formats instead: YYYY-MM-DD."}
	}
	return &parsed, nil
}

// dateTimeField is DRF's DateTimeField over ISO-8601, which takes a day on its own as midnight.
func dateTimeField(raw json.RawMessage) (*time.Time, []string) {
	if string(raw) == "null" {
		return nil, nil
	}
	const message = "Datetime has wrong format. Use one of these formats instead: YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z]."
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil, []string{message}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed, nil
		}
	}
	return nil, []string{message}
}

// integerField is DRF's IntegerField, which reads a number written as a string but refuses one with a fraction. A limit of zero on either side means the field carries no bound there.
func integerField(raw json.RawMessage, low, high int64) (*int64, []string) {
	if string(raw) == "null" {
		return nil, nil
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil, []string{"A valid integer is required."}
	}
	var text string
	switch typed := decoded.(type) {
	case string:
		text = typed
	case float64:
		text = strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return nil, []string{"A valid integer is required."}
	}
	text = strings.TrimSuffix(text, ".0")
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, []string{"A valid integer is required."}
	}
	if high > 0 && value > high {
		return nil, []string{"Ensure this value is less than or equal to " + strconv.FormatInt(high, 10) + "."}
	}
	if high > 0 && value < low {
		return nil, []string{"Ensure this value is greater than or equal to " + strconv.FormatInt(low, 10) + "."}
	}
	return &value, nil
}

func floatField(raw json.RawMessage) (*float64, []string) {
	if string(raw) == "null" {
		return nil, nil
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil, []string{"A valid number is required."}
	}
	switch typed := decoded.(type) {
	case float64:
		return &typed, nil
	case string:
		value, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return nil, []string{"A valid number is required."}
		}
		return &value, nil
	}
	return nil, []string{"A valid number is required."}
}

// booleanField is DRF's BooleanField, which takes the words and the numbers people write for true and false.
func booleanField(raw json.RawMessage) (*bool, []string) {
	if string(raw) == "null" {
		return nil, []string{"This field may not be null."}
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil, []string{"Must be a valid boolean."}
	}
	switch typed := decoded.(type) {
	case bool:
		return &typed, nil
	case float64:
		value := typed != 0
		if typed != 0 && typed != 1 {
			return nil, []string{"Must be a valid boolean."}
		}
		return &value, nil
	case string:
		switch strings.ToLower(typed) {
		case "t", "true", "1", "yes", "on":
			value := true
			return &value, nil
		case "f", "false", "0", "no", "off":
			value := false
			return &value, nil
		}
	}
	return nil, []string{"Must be a valid boolean."}
}

// primaryKeyField is DRF's PrimaryKeyRelatedField before the row is read: the value has to be an identifier at all.
func primaryKeyField(raw json.RawMessage) (*string, []string) {
	if string(raw) == "null" {
		return nil, nil
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil, []string{"“” is not a valid UUID."}
	}
	// RelatedField.run_validation turns an empty string into None before anything else looks at it: "We force empty strings to None values for relational fields."
	if text, isText := decoded.(string); isText && text == "" {
		return nil, nil
	}
	value, ok := decoded.(string)
	if !ok {
		return nil, []string{"“" + stringifyChoice(decoded) + "” is not a valid UUID."}
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil, []string{"“" + value + "” is not a valid UUID."}
	}
	canonical := parsed.String()
	return &canonical, nil
}

// primaryKeyList is the ListField the two sets are declared as. A bad entry is reported under its position rather than under the field, which is DRF's shape for a nested error.
func primaryKeyList(raw json.RawMessage) ([]string, any) {
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil, []string{`Expected a list of items but got type "str".`}
	}
	items, ok := decoded.([]any)
	if !ok {
		return nil, []string{`Expected a list of items but got type "` + pythonTypeName(decoded) + `".`}
	}
	values := []string{}
	failures := gin.H{}
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			failures[strconv.Itoa(index)] = []string{"“" + stringifyChoice(item) + "” is not a valid UUID."}
			continue
		}
		parsed, err := uuid.Parse(text)
		if err != nil {
			failures[strconv.Itoa(index)] = []string{"“" + text + "” is not a valid UUID."}
			continue
		}
		values = append(values, parsed.String())
	}
	if len(failures) > 0 {
		return nil, failures
	}
	return values, nil
}

// pythonTypeName is the name Python prints for a decoded JSON value, which is what the list error carries.
func pythonTypeName(value any) string {
	switch value.(type) {
	case string:
		return "str"
	case float64:
		return "float"
	case bool:
		return "bool"
	case map[string]any:
		return "dict"
	case nil:
		return "NoneType"
	}
	return "str"
}

// jsonValue keeps a payload's own bytes for a jsonb column.
func jsonValue(raw []byte) any {
	return auth.JSONValue(append([]byte(nil), raw...))
}
