// Package access holds the project membership predicate every application's queries are written around, because all three of them ask the same question and the answer is an authorization decision.
//
// Two columns on project_members look like two spellings of one thing and are not. `is_active = FALSE` means this person was removed from the project: it is written synchronously, by project member removal, by workspace member removal and by account deactivation, and none of those three touches deleted_at. `deleted_at IS NOT NULL` means the project or workspace the membership belonged to was soft-deleted: it is written by one path only, the worker's cascade over the relation graph, which runs after the delete has already answered and after the parent row's own deleted_at is set.
//
// So the membership test is is_active, and it is unconditional. The membership's own soft delete is a lagging copy of a parent-liveness signal, and whether a query carries it is a question about which queryset the query is written from: Django's default manager filters the model's own queryset and never a join traversed through it, so a predicate written from ProjectMember's manager carries deleted_at IS NULL and a relation traversal from another model's queryset does not. That is why the option below is an option rather than the default — the sites that leave it out are reproducing the port's rule, and the base row's own deleted_at, stamped by the same cascade, is what excludes a cascaded row from those queries anyway.
package access

// MemberJoin is the membership join a project-scoped queryset carries: the caller's own active membership of the project the base row belongs to. It is an inner join, so a row whose reader is not a member of its project does not survive it.
//
// alias and projectColumn say where the project id is read from, which is not always <alias>.project_id — a query over projects joins on p.id, and a page reaches its project through the link table, so it joins on pp.project_id. The member id stays a bind argument of the call site, which reads Joins(access.MemberJoin("i", "project_id"), user.ID).
func MemberJoin(alias, projectColumn string, options ...Option) string {
	var settings joinSettings
	for _, option := range options {
		option(&settings)
	}
	join := "JOIN project_members pm ON pm.project_id = " + alias + "." + projectColumn +
		" AND pm.member_id = ? AND pm.is_active = TRUE"
	if settings.membershipSoftDelete {
		join += " AND pm.deleted_at IS NULL"
	}
	return join
}

// Option narrows what MemberJoin writes. There is one, and the reason it exists is in this package's doc comment.
type Option func(*joinSettings)

// joinSettings is unexported so that the only options are the ones named here.
type joinSettings struct {
	membershipSoftDelete bool
}

// WithMembershipSoftDelete adds the membership row's own soft-delete check, for the queries whose predicate is written from project_members' own manager rather than traversed into from another model's queryset. It narrows nothing the parent's deleted_at does not narrow first, and it narrows it later, because the cascade that writes the one has already written the other.
func WithMembershipSoftDelete() Option {
	return func(settings *joinSettings) { settings.membershipSoftDelete = true }
}
