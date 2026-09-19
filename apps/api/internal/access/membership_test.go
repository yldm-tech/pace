package access

import (
	"strings"
	"testing"
)

// The expected strings here are the literals the call sites carried before they were replaced, copied out of them character for character. That is the point of the test: the join this package writes has to be the same SQL those queries have always sent, for each of the three shapes of project column the tree uses.
func TestTheMembershipJoinIsWhatTheCallSitesWrote(t *testing.T) {
	for _, test := range []struct {
		name string
		got  string
		want string
	}{
		{
			name: "the base row's project",
			got:  MemberJoin("i", "project_id"),
			want: "JOIN project_members pm ON pm.project_id = i.project_id AND pm.member_id = ? AND pm.is_active = TRUE",
		},
		{
			name: "a query over projects themselves",
			got:  MemberJoin("p", "id"),
			want: "JOIN project_members pm ON pm.project_id = p.id AND pm.member_id = ? AND pm.is_active = TRUE",
		},
		{
			name: "a page, which reaches its project through the link table",
			got:  MemberJoin("pp", "project_id"),
			want: "JOIN project_members pm ON pm.project_id = pp.project_id AND pm.member_id = ? AND pm.is_active = TRUE",
		},
		{
			name: "the membership's own soft delete, where the queryset is project_members'",
			got:  MemberJoin("l", "project_id", WithMembershipSoftDelete()),
			want: "JOIN project_members pm ON pm.project_id = l.project_id AND pm.member_id = ? AND pm.is_active = TRUE AND pm.deleted_at IS NULL",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Errorf("the join renders\n  %s\nwant\n  %s", test.got, test.want)
			}
		})
	}
}

// The option is the only thing that adds the membership's soft delete, so a site that does not pass it cannot acquire the filter by accident.
func TestTheMembershipSoftDeleteIsOptIn(t *testing.T) {
	if got := MemberJoin("i", "project_id"); strings.Contains(got, "pm.deleted_at") {
		t.Errorf("the plain join filters the membership's soft delete:\n  %s", got)
	}
	if got := MemberJoin("i", "project_id", WithMembershipSoftDelete()); !strings.Contains(got, "AND pm.deleted_at IS NULL") {
		t.Errorf("the option did not add the membership's soft delete:\n  %s", got)
	}
}
