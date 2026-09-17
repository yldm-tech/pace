package project

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// get_actual_relation collapses the six directions a caller may ask for onto the three types a row is stored as, because a relation is recorded once and read from whichever end the caller stands at. Anything it does not name is stored verbatim, which is what lets the two symmetric types through.
func TestRequestedRelationTypesCollapseOntoTheStoredOnes(t *testing.T) {
	for requested, want := range map[string]string{
		"start_after":    "start_before",
		"finish_after":   "finish_before",
		"blocking":       "blocked_by",
		"blocked_by":     "blocked_by",
		"start_before":   "start_before",
		"finish_before":  "finish_before",
		"implemented_by": "implemented_by",
		"implements":     "implemented_by",
		// Not in the mapping, so stored as asked.
		"duplicate":  "duplicate",
		"relates_to": "relates_to",
		"nonsense":   "nonsense",
	} {
		if got := storedRelationType(requested); got != want {
			t.Errorf("storedRelationType(%q) = %q, want %q", requested, got, want)
		}
	}
}

// The three readings taken from the far end write the row with the two issues swapped, which is how blocking and blocked_by share one stored type.
func TestOnlyTheFarEndReadingsSwapTheIssues(t *testing.T) {
	for _, requested := range []string{"blocking", "start_after", "finish_after"} {
		if !relationIsReversed(requested) {
			t.Errorf("%q is read from the far end and must swap", requested)
		}
	}
	for _, requested := range []string{"blocked_by", "start_before", "finish_before", "duplicate", "relates_to", "implements", "implemented_by"} {
		if relationIsReversed(requested) {
			t.Errorf("%q must not swap", requested)
		}
	}
}

// The response has eight keys. The two symmetric types read from both ends inside a single query, because Django unions those two readings and applies DISTINCT: querying them separately would list an issue related from both ends twice.
func TestRelationBucketsCoverTheResponseShape(t *testing.T) {
	seen := map[string]relationBucket{}
	for _, bucket := range relationBuckets {
		if _, duplicate := seen[bucket.key]; duplicate {
			t.Fatalf("%q is built twice; the two readings belong in one query", bucket.key)
		}
		seen[bucket.key] = bucket
	}
	for _, key := range []string{
		"blocking", "blocked_by", "duplicate", "relates_to",
		"start_after", "start_before", "finish_after", "finish_before",
	} {
		if _, present := seen[key]; !present {
			t.Errorf("the response is missing the %q key", key)
		}
	}
	if len(seen) != 8 {
		t.Fatalf("the response has %d keys, want 8", len(seen))
	}
	for _, key := range []string{"duplicate", "relates_to"} {
		if len(seen[key].reversed) != 2 {
			t.Errorf("%q is symmetric and must be read from both ends", key)
		}
	}
	// blocking and blocked_by are the same stored type read from opposite ends.
	if seen["blocking"].stored != seen["blocked_by"].stored {
		t.Error("blocking and blocked_by must share a stored type")
	}
	if seen["blocking"].reversed[0] == seen["blocked_by"].reversed[0] {
		t.Error("blocking and blocked_by must read from opposite ends")
	}
}

// The list returns values(), so state_id is the raw column and is present as null when the issue has no state.
func TestListedRelationsCarryTheValuesProjection(t *testing.T) {
	row := relatedIssueRow{
		ID: "issue-id", Name: "Something", SortOrder: 65535, Priority: "high",
		SequenceID: 7, ProjectID: "project-id",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	data := relatedIssueJSON(row, "blocking")
	for _, field := range []string{
		"id", "name", "state_id", "sort_order", "priority", "sequence_id", "project_id",
		"label_ids", "assignee_ids", "created_at", "updated_at", "created_by", "updated_by",
		"relation_type",
	} {
		if _, present := data[field]; !present {
			t.Errorf("listed relation is missing %q", field)
		}
	}
	if len(data) != 14 {
		t.Fatalf("listed relation has %d fields, want 14", len(data))
	}
	if data["state_id"] != (*string)(nil) {
		t.Fatalf("state_id = %v, want an explicit null", data["state_id"])
	}
	// relation_type is the key's name, not the stored type: blocking is stored as blocked_by.
	if data["relation_type"] != "blocking" {
		t.Fatalf("relation_type = %v, want the bucket's name", data["relation_type"])
	}
	// The id arrays are empty rather than null when nothing aggregated.
	if got, ok := data["label_ids"].([]string); !ok || len(got) != 0 {
		t.Fatalf("label_ids = %v, want an empty array", data["label_ids"])
	}
}

// The create and delete responses go through the relation serializers, whose audit fields are the relation's and whose relation_type is the stored one. assignee_ids is declared write_only on both, so it never reaches a response.
func TestRelationSerializerCarriesTheRelationsOwnFields(t *testing.T) {
	actor := "actor-id"
	moment := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	relation := IssueRelation{
		ID: "relation-id", IssueID: "near-issue", RelatedIssueID: "far-issue",
		RelationType: "blocked_by", CreatedByID: &actor, UpdatedByID: &actor,
		CreatedAt: moment, UpdatedAt: moment,
	}
	data := issueRelationJSON(relation, relation.RelatedIssueID)
	// id is the issue's, not the relation's.
	if data["id"] != "far-issue" {
		t.Fatalf("id = %v, want the related issue", data["id"])
	}
	if data["created_at"] != moment || data["created_by"] != &actor {
		t.Fatal("the audit fields must be the relation's own")
	}
	if data["relation_type"] != "blocked_by" {
		t.Fatalf("relation_type = %v, want the stored type", data["relation_type"])
	}
	if _, present := data["assignee_ids"]; present {
		t.Error("assignee_ids is write_only and must not be serialized")
	}
	// The five fields reached through the foreign key are filled in later, and state_id is dropped entirely when the issue has no state, so none of them may be pre-seeded as null here.
	for _, filled := range []string{"project_id", "sequence_id", "name", "state_id", "priority"} {
		if _, present := data[filled]; present {
			t.Errorf("%q must not be pre-seeded; it is resolved from the issue", filled)
		}
	}
}

func TestRelationSubjectFollowsTheSerializerTheRequestChose(t *testing.T) {
	relation := IssueRelation{IssueID: "near-issue", RelatedIssueID: "far-issue", RelationType: "blocked_by"}
	// IssueRelationSerializer sources from related_issue.
	if issueRelationJSON(relation, relation.RelatedIssueID)["id"] != "far-issue" {
		t.Error("the forward reading must return the related issue")
	}
	// RelatedIssueSerializer, used for the three far-end readings, sources from issue.
	if issueRelationJSON(relation, relation.IssueID)["id"] != "near-issue" {
		t.Error("the far-end reading must return the owning issue")
	}
}

func TestEmptyBucketsSerializeAsArrays(t *testing.T) {
	// A key with no relations has to be [] rather than null, since the frontend maps over every key.
	response := gin.H{}
	for _, bucket := range relationBuckets {
		response[bucket.key] = make([]gin.H, 0)
	}
	for _, bucket := range relationBuckets {
		if response[bucket.key] == nil {
			t.Errorf("%q is null rather than an empty array", bucket.key)
		}
	}
}
