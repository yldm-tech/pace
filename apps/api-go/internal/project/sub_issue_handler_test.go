package project

import (
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// The read route returns the values() projection, which is IssueSerializer's twenty-five fields plus state_group. description_html is not among them, so the sub-issue list does not carry issue bodies.
func TestSubIssueReadShapeIsValuesPlusStateGroup(t *testing.T) {
	group := "started"
	data := subIssueValuesJSON(subIssueRow{issueRow: sampleIssueRow(), StateGroup: &group}, time.UTC)
	for _, field := range []string{
		"id", "name", "state_id", "sort_order", "completed_at", "estimate_point",
		"priority", "start_date", "target_date", "sequence_id", "project_id",
		"parent_id", "cycle_id", "module_ids", "label_ids", "assignee_ids",
		"sub_issues_count", "created_at", "updated_at", "created_by", "updated_by",
		"attachment_count", "link_count", "is_draft", "archived_at", "state_group",
	} {
		if _, present := data[field]; !present {
			t.Errorf("sub-issue is missing %q", field)
		}
	}
	if _, present := data["description_html"]; present {
		t.Error("the values() projection does not select description_html")
	}
	if _, present := data["is_subscribed"]; present {
		t.Error("the values() projection does not select is_subscribed")
	}
	if len(data) != 26 {
		t.Fatalf("sub-issue has %d fields, want 26", len(data))
	}
}

// The assign route serializes plain model instances through IssueSerializer, and none of the seven annotated fields exist on an instance. DRF makes a read-only field not required, so get_attribute raises SkipField for each and the key is dropped rather than rendered as null.
func TestAssignResponseDropsEveryAnnotatedField(t *testing.T) {
	data := issueSerializerJSON(sampleIssueRow())
	for _, annotated := range []string{
		"cycle_id", "module_ids", "label_ids", "assignee_ids",
		"sub_issues_count", "attachment_count", "link_count",
		"description_html", "state_group",
	} {
		if _, present := data[annotated]; present {
			t.Errorf("%q is not on the instance, so DRF drops it", annotated)
		}
	}
	for _, field := range []string{
		"id", "name", "state_id", "sort_order", "completed_at", "estimate_point",
		"priority", "start_date", "target_date", "sequence_id", "project_id",
		"parent_id", "created_at", "updated_at", "created_by", "updated_by",
		"is_draft", "archived_at",
	} {
		if _, present := data[field]; !present {
			t.Errorf("assign response is missing %q", field)
		}
	}
	if len(data) != 18 {
		t.Fatalf("assign response has %d fields, want 18", len(data))
	}
}

func TestReadRouteRendersTimestampsInTheCallersTimezone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	data := subIssueValuesJSON(subIssueRow{issueRow: sampleIssueRow()}, shanghai)
	created, ok := data["created_at"].(time.Time)
	if !ok {
		t.Fatalf("created_at = %T, want a time", data["created_at"])
	}
	if name, _ := created.Zone(); name != "CST" {
		t.Fatalf("created_at is in %s, want the caller's zone", name)
	}
	// Only the instant's rendering moves, never the instant itself.
	if !created.Equal(sampleIssueRow().CreatedAt) {
		t.Fatal("the conversion changed the instant")
	}
	// completed_at and the date fields are not in the converter's list.
	if _, present := data["completed_at"].(time.Time); present {
		t.Fatal("completed_at was converted, but the endpoint converts only created_at and updated_at")
	}
}

func TestUserTimezoneFallsBackToUTCAndSurfacesUnknownZones(t *testing.T) {
	location, err := userLocation(&auth.User{})
	if err != nil || location != time.UTC {
		t.Fatalf("an unset timezone gave (%v, %v), want UTC", location, err)
	}
	// pytz raises UnknownTimeZoneError, which BaseAPIView turns into a 500, so this must not silently fall back.
	if _, err := userLocation(&auth.User{UserTimezone: "Mars/Olympus"}); err == nil {
		t.Fatal("an unknown timezone must be an error, not a silent fallback to UTC")
	}
}

// The distribution keys on the raw state group, and an issue with no state lands under the string Django's str(None) produces.
func TestStateDistributionKeysOnTheRawGroup(t *testing.T) {
	if groupKey(nil) != "None" {
		t.Fatalf("a null group renders as %q, want None", groupKey(nil))
	}
	group := "backlog"
	if groupKey(&group) != "backlog" {
		t.Fatal("a set group must render verbatim")
	}
}

func TestGroupingKeysMatchPythonStr(t *testing.T) {
	missing := (*string)(nil)
	for _, test := range []struct {
		in   any
		want string
	}{
		{in: nil, want: "None"},
		{in: missing, want: "None"},
		{in: "urgent", want: "urgent"},
		{in: true, want: "True"},
		{in: false, want: "False"},
	} {
		if got := pythonString(test.in); got != test.want {
			t.Errorf("pythonString(%v) = %q, want %q", test.in, got, test.want)
		}
	}
}

// issue_objects hides more than soft-deleted rows: triage, archived and draft issues, and issues whose project is archived. A null state has to pass the triage check, because Django's exclude() over a nullable join renders as NOT (group = 'triage' AND group IS NOT NULL).
func TestIssueObjectsPredicateCoversEveryExclusion(t *testing.T) {
	predicate := issueObjectsPredicate("sub")
	for _, fragment := range []string{
		"sub.deleted_at IS NULL",
		"IS DISTINCT FROM 'triage'",
		"sub.archived_at IS NULL",
		"p.archived_at IS NULL",
		"sub.is_draft = FALSE",
	} {
		if !strings.Contains(predicate, fragment) {
			t.Errorf("issue_objects predicate is missing %q:\n%s", fragment, predicate)
		}
	}
}

func TestGroupAccumulatorsIgnoreForeignValues(t *testing.T) {
	// The accumulators read back out of a gin.H, so a value of another type has to degrade to an empty slice rather than panic.
	if asStrings(gin.H{}) != nil || asMaps("not a slice") != nil {
		t.Fatal("the accumulators must ignore values they did not write")
	}
	if got := append(asStrings(nil), "a"); len(got) != 1 {
		t.Fatal("appending to a missing key must start a new slice")
	}
	if got := append(asMaps(nil), gin.H{}); len(got) != 1 {
		t.Fatal("appending to a missing key must start a new slice")
	}
}

// The annotation SQL has to keep the places Django does not filter soft-deleted rows: a model's default manager only filters that model's own queryset, never a join traversed through it, so a soft-deleted project membership still keeps its assignee in assignee_ids.
func TestAnnotationJoinsKeepDjangosMissingSoftDeleteFilters(t *testing.T) {
	annotations := subIssueAnnotations()
	for _, absent := range []string{"pm2.deleted_at", "m.deleted_at", "s.deleted_at"} {
		if strings.Contains(annotations, absent) {
			t.Errorf("the select filters %s, which Django's join does not", absent)
		}
	}
	// What it does filter, it must keep filtering.
	for _, present := range []string{
		"pm2.is_active = TRUE", "m.archived_at IS NULL",
		"ia.deleted_at IS NULL", "mi.deleted_at IS NULL", "il2.deleted_at IS NULL",
		"ci.deleted_at IS NULL", "il.deleted_at IS NULL", "fa.deleted_at IS NULL",
	} {
		if !strings.Contains(annotations, present) {
			t.Errorf("the select is missing %q", present)
		}
	}
	// The counts coalesce, because Django wraps each in Coalesce(..., 0); the arrays coalesce to an empty array for the same reason.
	if strings.Count(annotations, "COALESCE(") != 6 {
		t.Errorf("want six coalesced annotations, got %d", strings.Count(annotations, "COALESCE("))
	}
	// The child count applies the full issue_objects manager, not just the soft-delete filter.
	if !strings.Contains(annotations, issueObjectsPredicate("sub")) {
		t.Error("sub_issues_count must apply the whole issue_objects predicate")
	}
}
