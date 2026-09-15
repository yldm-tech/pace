package externalapi

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// Only three kinds are ever stored; their opposites are the same rows read from the other end.
func TestOnlyThreeRelationKindsAreStored(t *testing.T) {
	for requested, stored := range map[string]string{
		"blocking":       "blocked_by",
		"blocked_by":     "blocked_by",
		"start_after":    "start_before",
		"start_before":   "start_before",
		"finish_after":   "finish_before",
		"finish_before":  "finish_before",
		"implements":     "implemented_by",
		"implemented_by": "implemented_by",
		// Anything the table does not name is stored as it was given.
		"duplicate":  "duplicate",
		"relates_to": "relates_to",
		"nonsense":   "nonsense",
	} {
		if got := storedRelationType(requested); got != stored {
			t.Errorf("%q stores as %q, want %q", requested, got, stored)
		}
	}
	// The three that read backwards are written with the ends swapped.
	for _, requested := range []string{"blocking", "start_after", "finish_after"} {
		if !reversedRelationTypes[requested] {
			t.Errorf("%q is written backwards", requested)
		}
	}
	for _, requested := range []string{"blocked_by", "start_before", "finish_before", "duplicate", "relates_to"} {
		if reversedRelationTypes[requested] {
			t.Errorf("%q is written as it reads", requested)
		}
	}
}

// One stored row fills two buckets depending on which end the work item sits at.
func TestOneRowFillsTwoBuckets(t *testing.T) {
	const me = "me"
	rows := []relationRow{
		{RelationType: "blocked_by", IssueID: me, RelatedIssueID: "other", IssueProjectID: "p1", RelatedIssueProjectID: "p2"},
		{RelationType: "blocked_by", IssueID: "other", RelatedIssueID: me, IssueProjectID: "p2", RelatedIssueProjectID: "p1"},
	}
	grouped := groupRelations(rows, me)
	if len(grouped["blocked_by"].([]gin.H)) != 1 {
		t.Errorf("blocked_by has %v", grouped["blocked_by"])
	}
	if len(grouped["blocking"].([]gin.H)) != 1 {
		t.Errorf("blocking has %v", grouped["blocking"])
	}
	// Every bucket is present even when empty.
	if len(grouped) != 8 {
		t.Fatalf("there are %d buckets, want 8", len(grouped))
	}
	for _, bucket := range relationBuckets {
		if _, present := grouped[bucket]; !present {
			t.Errorf("the %q bucket is missing", bucket)
		}
	}
}

// Two kinds are deduplicated and four are not.
func TestOnlyTheSymmetricKindsAreDeduplicated(t *testing.T) {
	const me = "me"
	// The same pair recorded twice under a symmetric kind is reported once.
	duplicates := []relationRow{
		{RelationType: "duplicate", IssueID: me, RelatedIssueID: "other", RelatedIssueProjectID: "p2"},
		{RelationType: "duplicate", IssueID: "other", RelatedIssueID: me, IssueProjectID: "p2"},
	}
	if got := len(groupRelations(duplicates, me)["duplicate"].([]gin.H)); got != 1 {
		t.Errorf("a symmetric pair was reported %d times, want once", got)
	}
	// The same pair recorded twice under a directional kind is reported twice.
	directional := []relationRow{
		{RelationType: "start_before", IssueID: me, RelatedIssueID: "other", RelatedIssueProjectID: "p2"},
		{RelationType: "start_before", IssueID: me, RelatedIssueID: "other", RelatedIssueProjectID: "p2"},
	}
	if got := len(groupRelations(directional, me)["start_before"].([]gin.H)); got != 2 {
		t.Errorf("a directional pair was reported %d times, want twice", got)
	}
}

// An implemented_by relation is stored and never reported: the grouping has no branch for it.
func TestImplementedByIsStoredAndNeverReported(t *testing.T) {
	rows := []relationRow{{RelationType: "implemented_by", IssueID: "me", RelatedIssueID: "other"}}
	grouped := groupRelations(rows, "me")
	for _, bucket := range relationBuckets {
		if len(grouped[bucket].([]gin.H)) != 0 {
			t.Errorf("the %q bucket reported an implemented_by relation", bucket)
		}
	}
	// It is a kind the create will happily store.
	if storedRelationType("implements") != "implemented_by" {
		t.Error("implements is stored as implemented_by")
	}
}

// The two response shapes differ in which end they name, so the id in the body is the other party either way.
func TestTheTwoRelationShapesNameTheOtherParty(t *testing.T) {
	relation := IssueRelation{ID: "relation-id", IssueID: "written-from", RelatedIssueID: "written-to"}
	if issueRelationJSON(relation, false)["issue"] != "written-to" {
		t.Error("a forward relation names the related work item")
	}
	if issueRelationJSON(relation, true)["issue"] != "written-from" {
		t.Error("a reverse relation names the work item it was written from")
	}
}
