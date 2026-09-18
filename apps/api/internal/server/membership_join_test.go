package server

import (
	"regexp"
	"strings"
	"testing"
)

// TestTheMembershipJoinIsNotWrittenOutByHand keeps the join that gates every project-scoped queryset in one place.
//
// It was written out as a literal at thirty-six call sites in three packages, which is how the five that carry the membership's own soft delete came to be indistinguishable from the thirty-one that leave it out on purpose: the difference was a few characters inside otherwise identical strings. access.MemberJoin writes it now, and the exception is an argument you can see. A new query that spells the join out again is not wrong SQL, but it puts the semantics back where nobody can find them, so it fails here instead.
//
// The pattern is the shape that joins the project id first and the member id second, which is the authorization join. The assignee joins in the work-item annotations (pm2.member_id = ia.assignee_id) decide which assignees to render rather than who may read, and they are deliberately not this.
func TestTheMembershipJoinIsNotWrittenOutByHand(t *testing.T) {
	handWritten := regexp.MustCompile(`JOIN project_members \w+ ON \w+\.project_id = \w+\.\w+ AND \w+\.member_id = \?`)
	if offenders := walk(t, handWritten); len(offenders) > 0 {
		t.Errorf("the membership join belongs to access.MemberJoin, which is where its two soft-delete rules are written down:\n  %s", strings.Join(offenders, "\n  "))
	}
}
