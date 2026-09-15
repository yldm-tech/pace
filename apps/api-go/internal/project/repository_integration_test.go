package project

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TestProjectModelsAgainstDjangoSchema is opt-in because it writes to the
// configured database. Every write is rolled back, and Go never creates or
// migrates tables.
func TestProjectModelsAgainstDjangoSchema(t *testing.T) {
	databaseURL := os.Getenv("PROJECT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set PROJECT_TEST_DATABASE_URL to a disposable database with the Django schema")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("access integration database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := sqlDatabase.PingContext(ctx); err != nil {
		t.Fatalf("ping integration database: %v", err)
	}

	transaction := database.WithContext(ctx).Begin()
	if transaction.Error != nil {
		t.Fatalf("begin integration transaction: %v", transaction.Error)
	}
	t.Cleanup(func() { _ = transaction.Rollback().Error })

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	users := auth.NewGORMRepository(transaction, false, "pace-project-integration-secret")
	user, err := users.CreateUser(ctx, "go-project-"+suffix+"@pace.invalid", "!", true, true)
	if err != nil {
		t.Fatalf("create integration user: %v", err)
	}

	now := time.Now().UTC()
	workspaceID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	slug := "go-project-" + suffix
	workspace := testWorkspace{
		ID: workspaceID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		Name: "Go Project WS " + suffix, OwnerID: user.ID, Slug: slug,
		Timezone: "UTC", BackgroundColor: "#3f76ff",
	}
	if err := transaction.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace through Django schema: %v", err)
	}
	workspaceMemberID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	member := testWorkspaceMember{
		ID: workspaceMemberID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: workspaceID, MemberID: user.ID, Role: roleAdmin, IsActive: true,
		ViewProps: emptyJSON(), DefaultProps: emptyJSON(), IssueProps: emptyJSON(),
		GettingStartedChecklist: emptyJSON(), Tips: emptyJSON(), ExploredFeatures: emptyJSON(),
	}
	if err := transaction.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member through Django schema: %v", err)
	}

	handler := NewHandler(transaction, nil, Settings{})
	projectID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	project := Project{
		ID: projectID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: workspaceID, Name: "Go Project " + suffix, Identifier: "GP" + suffix[len(suffix)-6:],
		Network: 2, PageView: true, Timezone: "UTC", LogoProps: emptyJSON(),
	}
	if err := transaction.Create(&project).Error; err != nil {
		t.Fatalf("create project through Django schema: %v", err)
	}

	identifier := ProjectIdentifier{
		CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: &workspaceID, ProjectID: project.ID, Name: project.Identifier,
	}
	if err := transaction.Create(&identifier).Error; err != nil {
		t.Fatalf("create project identifier through Django schema: %v", err)
	}
	if identifier.ID == 0 {
		t.Fatal("project identifier should receive its BigAutoField primary key")
	}

	if err := handler.addProjectAdmin(transaction, project, user.ID, user.ID, now); err != nil {
		t.Fatalf("create project member through Django schema: %v", err)
	}
	var property ProjectUserProperty
	err = transaction.Where("project_id = ? AND user_id = ? AND deleted_at IS NULL", project.ID, user.ID).Take(&property).Error
	if err != nil {
		t.Fatalf("read seeded project user property: %v", err)
	}
	if property.SortOrder != 65535 {
		t.Fatalf("first project sort order = %v, want the 65535 default", property.SortOrder)
	}

	if err := handler.createDefaultStates(transaction, project, user.ID, now); err != nil {
		t.Fatalf("create default states through Django schema: %v", err)
	}
	var states []State
	err = transaction.Where("project_id = ? AND deleted_at IS NULL", project.ID).Order("sequence").Find(&states).Error
	if err != nil {
		t.Fatalf("read default states: %v", err)
	}
	if len(states) != len(defaultStates) {
		t.Fatalf("default states = %d, want %d", len(states), len(defaultStates))
	}
	for index, state := range states {
		if state.Name != defaultStates[index].Name || state.Group != defaultStates[index].Group {
			t.Fatalf("default state %d = %#v", index, state)
		}
		// bulk_create bypasses State.save, so the slug stays empty.
		if state.Slug != "" {
			t.Fatalf("default state %d slug = %q, want the empty bulk_create value", index, state.Slug)
		}
	}

	row, found, err := handler.projectRowByID(ctx, slug, project.ID, user.ID, true)
	if err != nil || !found {
		t.Fatalf("query annotated project: %v, found=%v", err, found)
	}
	if row.MemberRole == nil || *row.MemberRole != roleAdmin {
		t.Fatalf("annotated member role = %#v", row.MemberRole)
	}
	if row.IsFavorite {
		t.Fatal("a fresh project should not be annotated as a favorite")
	}
	if row.SortOrder == nil || *row.SortOrder != 65535 {
		t.Fatalf("annotated sort order = %#v", row.SortOrder)
	}
	if row.Anchor != nil {
		t.Fatalf("annotated anchor = %#v, want none without a deploy board", row.Anchor)
	}

	data, err := handler.projectJSON(ctx, row)
	if err != nil {
		t.Fatalf("serialize project: %v", err)
	}
	if data["name"] != project.Name || data["next_work_item_sequence"] != int64(1) {
		t.Fatalf("serialized project = %#v", data)
	}
	members, ok := data["members"].([]string)
	if !ok || len(members) != 1 || members[0] != user.ID {
		t.Fatalf("serialized members = %#v", data["members"])
	}

	detail, err := handler.projectDetailJSON(ctx, project)
	if err != nil {
		t.Fatalf("serialize project detail: %v", err)
	}
	if detail["workspace_detail"] == nil || detail["inbox_view"] != project.IntakeView {
		t.Fatalf("serialized project detail = %#v", detail)
	}

	// The member routes read through the same rows the create flow seeds.
	projectMembers, err := handler.activeProjectMembers(ctx, slug, project.ID)
	if err != nil {
		t.Fatalf("list project members through Django schema: %v", err)
	}
	if len(projectMembers) != 1 || projectMembers[0].MemberID != user.ID || projectMembers[0].Role != roleAdmin {
		t.Fatalf("project members = %#v", projectMembers)
	}
	if serialized := projectMemberRoleJSON(projectMembers[0]); serialized["original_role"] != roleAdmin {
		t.Fatalf("serialized member role = %#v", serialized)
	}
	memberData, err := handler.projectMemberJSON(ctx, projectMembers[0], true)
	if err != nil {
		t.Fatalf("serialize project member: %v", err)
	}
	if memberData["project"] == nil || memberData["workspace"] == nil || memberData["member"] == nil {
		t.Fatalf("serialized project member = %#v", memberData)
	}
	roleRow, found, err := handler.activeProjectMember(ctx, slug, project.ID, user.ID)
	if err != nil || !found || roleRow.Role != roleAdmin {
		t.Fatalf("active project member = %#v, found=%v, err=%v", roleRow, found, err)
	}
	sortOrders, err := handler.minimumPropertySortOrders(transaction, workspaceID, []string{user.ID})
	if err != nil {
		t.Fatalf("read minimum sort orders: %v", err)
	}
	if sortOrders[user.ID] != 65535 {
		t.Fatalf("minimum sort order = %v", sortOrders[user.ID])
	}

	// Labels exercise the sort_order rule in Label.save.
	firstOrder, err := handler.nextLabelSortOrder(ctx, project.ID)
	if err != nil {
		t.Fatalf("compute the first label sort order: %v", err)
	}
	if firstOrder != 65535 {
		t.Fatalf("first label sort order = %v, want the 65535 default", firstOrder)
	}
	labelID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	label := Label{
		ID: labelID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		WorkspaceID: workspaceID, ProjectID: &project.ID, Name: "Bug " + suffix,
		Color: "#FF0000", SortOrder: firstOrder,
	}
	if err := transaction.Create(&label).Error; err != nil {
		t.Fatalf("create label through Django schema: %v", err)
	}
	secondOrder, err := handler.nextLabelSortOrder(ctx, project.ID)
	if err != nil {
		t.Fatalf("compute the second label sort order: %v", err)
	}
	if secondOrder != firstOrder+10000 {
		t.Fatalf("second label sort order = %v, want %v", secondOrder, firstOrder+10000)
	}
	// validate_name is case insensitive within the project.
	taken, err := handler.labelNameTaken(ctx, project.ID, strings.ToUpper(label.Name), "")
	if err != nil {
		t.Fatalf("check the label name: %v", err)
	}
	if !taken {
		t.Fatal("the label name check should be case insensitive")
	}
	if taken, err := handler.labelNameTaken(ctx, project.ID, label.Name, label.ID); err != nil || taken {
		t.Fatalf("excluding the row itself should clear the name: %v, %v", taken, err)
	}
	if serialized := labelJSON(label); serialized["sort_order"] != firstOrder {
		t.Fatalf("serialized label = %#v", serialized)
	}

	// The issue detail queryset has to survive contact with the real schema:
	// every table and column its annotations name must exist.
	issueID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Exec(
		`INSERT INTO issues (id, created_at, updated_at, created_by_id, project_id, workspace_id,
			name, description_json, description_html, priority, sequence_id, sort_order, is_draft)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '{}', '<p></p>', 'none', 1, 65535, FALSE)`,
		issueID, now, now, user.ID, project.ID, workspaceID, "Integration issue "+suffix,
	).Error
	if err != nil {
		t.Fatalf("create issue through Django schema: %v", err)
	}
	issueRowResult, found, err := handler.issueDetailRow(ctx, slug, project.ID, issueID, user.ID)
	if err != nil || !found {
		t.Fatalf("read the annotated issue: %v, found=%v", err, found)
	}
	if issueRowResult.Name != "Integration issue "+suffix || issueRowResult.SequenceID != 1 {
		t.Fatalf("annotated issue = %#v", issueRowResult.Issue)
	}
	// Every annotation should come back empty rather than failing.
	if countOrZero(issueRowResult.LinkCount) != 0 || countOrZero(issueRowResult.AttachmentCount) != 0 || countOrZero(issueRowResult.SubIssuesCount) != 0 {
		t.Fatalf("counts = %v %v %v", issueRowResult.LinkCount, issueRowResult.AttachmentCount, issueRowResult.SubIssuesCount)
	}
	if len(issueRowResult.LabelIDs) != 0 || len(issueRowResult.AssigneeIDs) != 0 || len(issueRowResult.ModuleIDs) != 0 {
		t.Fatalf("id arrays = %v %v %v", issueRowResult.LabelIDs, issueRowResult.AssigneeIDs, issueRowResult.ModuleIDs)
	}
	if issueRowResult.IsSubscribed {
		t.Fatal("a fresh issue should not be annotated as subscribed")
	}
	serializedIssue := issueDetailJSON(issueRowResult, true)
	if serializedIssue["sequence_id"] != 1 {
		t.Fatalf("serialized issue = %#v", serializedIssue)
	}
	// Attaching the label created above must show up in the aggregation.
	issueLabelID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Create(&IssueLabel{
		ID: issueLabelID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: project.ID, WorkspaceID: workspaceID, IssueID: issueID, LabelID: label.ID,
	}).Error
	if err != nil {
		t.Fatalf("attach a label through Django schema: %v", err)
	}
	issueRowResult, _, err = handler.issueDetailRow(ctx, slug, project.ID, issueID, user.ID)
	if err != nil {
		t.Fatalf("re-read the annotated issue: %v", err)
	}
	if len(issueRowResult.LabelIDs) != 1 || issueRowResult.LabelIDs[0] != label.ID {
		t.Fatalf("label_ids = %v, want the attached label", issueRowResult.LabelIDs)
	}

	// Sub-issues: the annotated read has to agree with the issue_objects manager, which hides more than soft-deleted rows.
	insertIssue := func(name string, stateID *string, isDraft bool, archived bool, createdAt time.Time) string {
		childID, err := newUUID()
		if err != nil {
			t.Fatal(err)
		}
		var archivedAt *time.Time
		if archived {
			archivedAt = &createdAt
		}
		err = transaction.Exec(
			`INSERT INTO issues (id, created_at, updated_at, created_by_id, project_id, workspace_id,
				name, description_json, description_html, priority, sequence_id, sort_order, is_draft,
				parent_id, state_id, archived_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '{}', '<p></p>', 'none', 1, 65535, ?, ?, ?, ?)`,
			childID, createdAt, createdAt, user.ID, project.ID, workspaceID, name,
			isDraft, issueID, stateID, archivedAt,
		).Error
		if err != nil {
			t.Fatalf("create sub-issue through Django schema: %v", err)
		}
		return childID
	}
	startedState := states[2].ID
	older := now.Add(-time.Hour)
	visibleChild := insertIssue("Visible child "+suffix, &startedState, false, false, older)
	newestChild := insertIssue("Newest child "+suffix, nil, false, false, now)
	insertIssue("Draft child "+suffix, &startedState, true, false, now)
	archivedChild := insertIssue("Archived child "+suffix, &startedState, false, true, now)

	// The default states already include the triage one, which issue_objects excludes.
	triageState := states[len(states)-1]
	if triageState.Group != "triage" {
		t.Fatalf("last default state = %q, want the triage one", triageState.Group)
	}
	insertIssue("Triage child "+suffix, &triageState.ID, false, false, now)

	subIssues, err := handler.subIssueRows(ctx, slug, project.ID, issueID, "-created_at")
	if err != nil {
		t.Fatalf("read sub-issues: %v", err)
	}
	if len(subIssues) != 2 {
		names := make([]string, 0, len(subIssues))
		for _, sub := range subIssues {
			names = append(names, sub.Name)
		}
		t.Fatalf("sub-issues = %v, want only the two issue_objects rows", names)
	}
	// -created_at is the default ordering, so the newest child comes first.
	if subIssues[0].ID != newestChild || subIssues[1].ID != visibleChild {
		t.Fatalf("sub-issue order = %s, %s; want newest first", subIssues[0].Name, subIssues[1].Name)
	}
	// state_group is annotated off the joined state, and is null when the issue has none.
	if subIssues[0].StateGroup != nil {
		t.Fatalf("state_group = %v, want null for a stateless issue", *subIssues[0].StateGroup)
	}
	if subIssues[1].StateGroup == nil || *subIssues[1].StateGroup != states[2].Group {
		t.Fatalf("state_group = %v, want %q", subIssues[1].StateGroup, states[2].Group)
	}
	// Unlike the detail queryset, every count here coalesces to zero rather than null.
	for _, sub := range subIssues {
		if sub.LinkCount == nil || sub.AttachmentCount == nil || sub.SubIssuesCount == nil {
			t.Fatalf("sub-issue %s has a null count, but the endpoint coalesces every one", sub.Name)
		}
		if countOrZero(sub.SubIssuesCount) != 0 || len(sub.LabelIDs) != 0 || len(sub.AssigneeIDs) != 0 {
			t.Fatalf("sub-issue %s = %#v", sub.Name, sub)
		}
	}
	// Ordering by a related minimum has to survive contact with the real schema.
	if _, err := handler.subIssueRows(ctx, slug, project.ID, issueID, "-labels__name"); err != nil {
		t.Fatalf("order sub-issues by label name: %v", err)
	}
	for _, orderBy := range []string{"priority", "-state__group", "state__name", "-assignees__first_name", "issue_module__module__name", "-sequence_id"} {
		if _, err := handler.subIssueRows(ctx, slug, project.ID, issueID, orderBy); err != nil {
			t.Fatalf("order sub-issues by %s: %v", orderBy, err)
		}
	}
	// The assign route re-reads by id, which must apply the same manager.
	byID, err := handler.subIssuesByID(ctx, slug, project.ID, []string{visibleChild, newestChild})
	if err != nil || len(byID) != 2 {
		t.Fatalf("re-read sub-issues by id: %v, got %d", err, len(byID))
	}

	// Issue relations: the list reads from whichever end of the row the bucket names, and the create path collapses the requested type onto the stored one.
	relationID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	// visibleChild blocks the parent issue, stored as the parent being blocked_by it.
	err = transaction.Create(&IssueRelation{
		ID: relationID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: project.ID, WorkspaceID: workspaceID,
		IssueID: issueID, RelatedIssueID: visibleChild, RelationType: "blocked_by",
	}).Error
	if err != nil {
		t.Fatalf("create issue relation through Django schema: %v", err)
	}

	blockedBy, err := handler.relatedIssues(ctx, slug, issueID, relationBucket{key: "blocked_by", stored: "blocked_by", reversed: []bool{false}})
	if err != nil {
		t.Fatalf("read the blocked_by relations: %v", err)
	}
	if len(blockedBy) != 1 || blockedBy[0].ID != visibleChild {
		t.Fatalf("blocked_by = %v, want the related child", blockedBy)
	}
	if blockedBy[0].LabelIDs == nil || len(blockedBy[0].LabelIDs) != 0 {
		t.Fatalf("label_ids = %v, want an empty array rather than null", blockedBy[0].LabelIDs)
	}
	// Read from the other end, the same row is the child's blocking relation and not the parent's.
	blocking, err := handler.relatedIssues(ctx, slug, issueID, relationBucket{key: "blocking", stored: "blocked_by", reversed: []bool{true}})
	if err != nil {
		t.Fatalf("read the blocking relations: %v", err)
	}
	if len(blocking) != 0 {
		t.Fatalf("blocking = %v, want nothing from this end", blocking)
	}
	childBlocking, err := handler.relatedIssues(ctx, slug, visibleChild, relationBucket{key: "blocking", stored: "blocked_by", reversed: []bool{true}})
	if err != nil {
		t.Fatalf("read the child's blocking relations: %v", err)
	}
	if len(childBlocking) != 1 || childBlocking[0].ID != issueID {
		t.Fatalf("the child's blocking = %v, want the parent", childBlocking)
	}

	// A symmetric type reads from both ends inside one query, so a pair related in both directions is returned once.
	reverseID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Create(&IssueRelation{
		ID: reverseID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: project.ID, WorkspaceID: workspaceID,
		IssueID: newestChild, RelatedIssueID: issueID, RelationType: "relates_to",
	}).Error
	if err != nil {
		t.Fatalf("create the reverse relation: %v", err)
	}
	forwardID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Create(&IssueRelation{
		ID: forwardID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: project.ID, WorkspaceID: workspaceID,
		IssueID: issueID, RelatedIssueID: newestChild, RelationType: "relates_to",
	}).Error
	if err != nil {
		t.Fatalf("create the forward relation: %v", err)
	}
	relatesTo, err := handler.relatedIssues(ctx, slug, issueID, relationBucket{key: "relates_to", stored: "relates_to", reversed: []bool{false, true}})
	if err != nil {
		t.Fatalf("read the relates_to relations: %v", err)
	}
	if len(relatesTo) != 1 || relatesTo[0].ID != newestChild {
		t.Fatalf("relates_to = %v, want the one issue related from both ends", relatesTo)
	}

	// The serializer path follows the foreign key, and drops state_id when the issue has no state.
	serializedRelations, err := handler.relationSerializerData(ctx, []IssueRelation{{
		ID: relationID, IssueID: issueID, RelatedIssueID: newestChild, RelationType: "blocked_by",
		CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
	}}, false)
	if err != nil || len(serializedRelations) != 1 {
		t.Fatalf("serialize the relation: %v, got %d", err, len(serializedRelations))
	}
	if serializedRelations[0]["name"] != "Newest child "+suffix {
		t.Fatalf("serialized relation = %#v", serializedRelations[0])
	}
	if _, present := serializedRelations[0]["state_id"]; present {
		t.Fatal("the newest child has no state, so state_id must be absent rather than null")
	}
	withState, err := handler.relationSerializerData(ctx, []IssueRelation{{
		ID: relationID, IssueID: issueID, RelatedIssueID: visibleChild, RelationType: "blocked_by",
		CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID, UpdatedByID: &user.ID,
	}}, false)
	if err != nil {
		t.Fatalf("serialize the stated relation: %v", err)
	}
	if withState[0]["state_id"] != states[2].ID {
		t.Fatalf("state_id = %v, want the child's state", withState[0]["state_id"])
	}

	// bulk_create(ignore_conflicts=True) is how the create path writes, so re-adding a pair that already exists must be a silent skip rather than an error — and must not leave a second row behind.
	repeatID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&IssueRelation{
		ID: repeatID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		ProjectID: project.ID, WorkspaceID: workspaceID,
		IssueID: issueID, RelatedIssueID: visibleChild, RelationType: "duplicate",
	}).Error
	if err != nil {
		t.Fatalf("re-adding an existing pair must be ignored, not rejected: %v", err)
	}
	var pairCount int64
	err = transaction.Model(&IssueRelation{}).
		Where("issue_id = ? AND related_issue_id = ? AND deleted_at IS NULL", issueID, visibleChild).
		Count(&pairCount).Error
	if err != nil {
		t.Fatal(err)
	}
	if pairCount != 1 {
		t.Fatalf("the pair exists %d times, want the conflict to have been ignored", pairCount)
	}

	// Archiving: the state group gate, the write Issue.save performs, and the queryset that can reach an archived row.
	completedState := states[3]
	if completedState.Group != "completed" {
		t.Fatalf("state 3 is %q, want completed", completedState.Group)
	}
	archivableID := insertIssue("Archivable "+suffix, &completedState.ID, false, false, now)
	var archivable Issue
	if err := transaction.Where("id = ?", archivableID).Take(&archivable).Error; err != nil {
		t.Fatal(err)
	}
	// The gate reads the group through the state, batched the way select_related does.
	groups, err := handler.issueStateGroups(ctx, []Issue{archivable})
	if err != nil {
		t.Fatalf("read the state groups: %v", err)
	}
	if groups[archivableID] != "completed" {
		t.Fatalf("state group = %q, want completed", groups[archivableID])
	}
	if !archivableStateGroups[groups[archivableID]] {
		t.Fatal("a completed issue must be archivable")
	}
	// A stateless issue is absent from the map, which is the case Django turns into an AttributeError.
	var stateless Issue
	if err := transaction.Where("id = ?", newestChild).Take(&stateless).Error; err != nil {
		t.Fatal(err)
	}
	if statelessGroups, err := handler.issueStateGroups(ctx, []Issue{stateless}); err != nil || len(statelessGroups) != 0 {
		t.Fatalf("a stateless issue must have no group: %v, %v", statelessGroups, err)
	}

	archiveDate := now.Format("2006-01-02")
	if err := handler.writeArchivedAt(ctx, archivable, &archiveDate, now.Add(time.Minute)); err != nil {
		t.Fatalf("archive the issue: %v", err)
	}
	var archived Issue
	if err := transaction.Where("id = ?", archivableID).Take(&archived).Error; err != nil {
		t.Fatal(err)
	}
	if archived.ArchivedAt == nil || archived.ArchivedAt.Format("2006-01-02") != archiveDate {
		t.Fatalf("archived_at = %v, want the bare date", archived.ArchivedAt)
	}
	// save writes the whole instance, so the timestamp moves and description_stripped is recomputed.
	if !archived.UpdatedAt.After(archivable.UpdatedAt) {
		t.Fatal("archiving must move updated_at, because save writes the whole instance")
	}
	if archived.DescriptionStripped == nil || *archived.DescriptionStripped != "" {
		// The fixture's description_html is the empty paragraph Django defaults to, which strips to nothing.
		t.Fatalf("description_stripped = %v, want the stripped body", archived.DescriptionStripped)
	}
	// completed_at must not move: _sync_completed_at returns early unless the state itself changed.
	if (archived.CompletedAt == nil) != (archivable.CompletedAt == nil) {
		t.Fatal("archiving must not touch completed_at")
	}

	// issue_objects hides the archived row, so the archive route reads through the plain manager instead.
	stillListed, err := handler.subIssueRows(ctx, slug, project.ID, issueID, "-created_at")
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range stillListed {
		if sub.ID == archivableID {
			t.Fatal("an archived issue must not appear through issue_objects")
		}
	}
	var reachable int64
	err = transaction.Model(&Issue{}).
		Where("id = ? AND deleted_at IS NULL AND archived_at IS NOT NULL", archivableID).Count(&reachable).Error
	if err != nil {
		t.Fatal(err)
	}
	if reachable != 1 {
		t.Fatal("the unarchive queryset must still reach the archived row")
	}

	// Unarchiving clears the column back to null. The column is read on its own rather than through a struct scan, so a failure here names the database's value rather than whatever a reused destination happened to be holding.
	if err := handler.writeArchivedAt(ctx, archived, nil, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("unarchive the issue: %v", err)
	}
	var stillArchived int64
	err = transaction.Session(&gorm.Session{}).Model(&Issue{}).
		Where("id = ? AND archived_at IS NOT NULL", archivableID).Count(&stillArchived).Error
	if err != nil {
		t.Fatal(err)
	}
	if stillArchived != 0 {
		t.Fatal("archived_at is still set in the database after unarchiving")
	}

	// Attachments: the row reserved by the create route, the flag the upload callback moves, and the list that hides what was never uploaded.
	assetID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	entityType := attachmentEntityType
	attributes, err := marshalUnescaped(map[string]any{"name": "report.pdf", "type": "application/pdf", "size": 1024})
	if err != nil {
		t.Fatal(err)
	}
	asset := FileAsset{
		ID: assetID, CreatedAt: now, UpdatedAt: now, CreatedByID: &user.ID,
		Attributes: auth.JSONValue(attributes), Asset: workspaceID + "/deadbeef-report.pdf", Size: 1024,
		WorkspaceID: &workspaceID, ProjectID: &project.ID, IssueID: &issueID,
		EntityType: &entityType, StorageMetadata: auth.JSONValue([]byte("{}")),
	}
	if err := transaction.Create(&asset).Error; err != nil {
		t.Fatalf("create attachment through Django schema: %v", err)
	}
	// A reserved row is not uploaded yet, so it must not be listed and its metadata must read as missing.
	if asset.IsUploaded {
		t.Fatal("a freshly reserved attachment must not be marked uploaded")
	}
	var reserved FileAsset
	if err := transaction.Where("id = ?", assetID).Take(&reserved).Error; err != nil {
		t.Fatal(err)
	}
	if reserved.HasStorageMetadata() {
		t.Fatalf("storage_metadata = %s, want the empty default to read as missing", reserved.StorageMetadata)
	}
	if attachmentName(reserved) != "report.pdf" {
		t.Fatalf("attachment name = %q", attachmentName(reserved))
	}
	var uploadedCount int64
	err = transaction.Session(&gorm.Session{}).Model(&FileAsset{}).
		Where("issue_id = ? AND entity_type = ? AND is_uploaded = TRUE AND deleted_at IS NULL", issueID, attachmentEntityType).
		Count(&uploadedCount).Error
	if err != nil {
		t.Fatal(err)
	}
	if uploadedCount != 0 {
		t.Fatalf("%d attachments are listed, want none before the upload completes", uploadedCount)
	}
	// Once the flag moves the attachment is listed, and the annotated count on the issue picks it up.
	err = transaction.Model(&FileAsset{}).Where("id = ?", assetID).
		Updates(map[string]any{"is_uploaded": true, "updated_at": now}).Error
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.Session(&gorm.Session{}).Model(&FileAsset{}).
		Where("issue_id = ? AND entity_type = ? AND is_uploaded = TRUE AND deleted_at IS NULL", issueID, attachmentEntityType).
		Count(&uploadedCount).Error
	if err != nil {
		t.Fatal(err)
	}
	if uploadedCount != 1 {
		t.Fatalf("%d attachments are listed after the upload, want 1", uploadedCount)
	}
	annotated, _, err := handler.issueDetailRow(ctx, slug, project.ID, issueID, user.ID)
	if err != nil {
		t.Fatalf("re-read the annotated issue: %v", err)
	}
	if countOrZero(annotated.AttachmentCount) != 1 {
		t.Fatalf("attachment_count = %v, want 1", annotated.AttachmentCount)
	}
	// The serializer is fields = "__all__" plus asset_url, and the url points back at the download route.
	serializedAsset := attachmentJSON(reserved, slug)
	wantURL := "/api/assets/v2/workspaces/" + slug + "/projects/" + project.ID + "/issues/" + issueID + "/attachments/" + assetID + "/"
	if serializedAsset["asset_url"] != wantURL {
		t.Fatalf("asset_url = %v, want %q", serializedAsset["asset_url"], wantURL)
	}
	// Deleting flags the row rather than removing it, and the annotated count drops again.
	err = transaction.Model(&FileAsset{}).Where("id = ?", assetID).
		Updates(map[string]any{"is_deleted": true, "deleted_at": now, "updated_at": now}).Error
	if err != nil {
		t.Fatal(err)
	}
	annotated, _, err = handler.issueDetailRow(ctx, slug, project.ID, issueID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countOrZero(annotated.AttachmentCount) != 0 {
		t.Fatalf("attachment_count = %v, want 0 once the attachment is flagged deleted", annotated.AttachmentCount)
	}

	// Activity history: the membership and archive gates, the four hidden fields, and the oldest-first ordering.
	writeActivity := func(field string, createdAt time.Time) string {
		activityID, err := newUUID()
		if err != nil {
			t.Fatal(err)
		}
		row := IssueActivity{
			ID: activityID, CreatedAt: createdAt, UpdatedAt: createdAt, CreatedByID: &user.ID,
			ProjectID: project.ID, WorkspaceID: workspaceID, IssueID: &issueID,
			Verb: "updated", ActorID: &user.ID, Attachments: pq.StringArray{},
		}
		if field != "" {
			row.Field = &field
		}
		if err := transaction.Create(&row).Error; err != nil {
			t.Fatalf("create activity through Django schema: %v", err)
		}
		return activityID
	}
	newest := writeActivity("priority", now)
	oldest := writeActivity("state", now.Add(-time.Hour))
	// Each of the four hidden fields must be excluded, and a null field must not be.
	for _, hidden := range hiddenActivityFields {
		writeActivity(hidden, now)
	}
	nullField := writeActivity("", now.Add(-2*time.Hour))

	history, err := handler.issueActivityRows(ctx, slug, project.ID, issueID, user.ID, nil)
	if err != nil {
		t.Fatalf("read the activity history: %v", err)
	}
	if len(history) != 3 {
		fields := make([]string, 0, len(history))
		for _, row := range history {
			fields = append(fields, groupKey(row.Field))
		}
		t.Fatalf("history has %v, want the two named fields and the null one", fields)
	}
	// Oldest first, unlike the model's own ordering.
	if history[0].ID != nullField || history[1].ID != oldest || history[2].ID != newest {
		t.Fatal("the history must be ordered oldest first")
	}
	// A null field survives the exclusion, because Django's NOT IN over a nullable column keeps it.
	if history[0].Field != nil {
		t.Fatalf("the first row is %v, want the null-field activity", history[0].Field)
	}

	// created_at__gt narrows it.
	cutoff := now.Add(-30 * time.Minute)
	recent, err := handler.issueActivityRows(ctx, slug, project.ID, issueID, user.ID, &cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].ID != newest {
		t.Fatalf("the filtered history has %d rows, want only the newest", len(recent))
	}

	// The serializer fills the nested details from the real rows.
	serializedHistory, err := handler.serializeActivities(ctx, history, true)
	if err != nil {
		t.Fatalf("serialize the history: %v", err)
	}
	if len(serializedHistory) != 3 {
		t.Fatalf("serialized %d activities", len(serializedHistory))
	}
	first := serializedHistory[0]
	if len(first) != 25 {
		t.Fatalf("serialized activity has %d fields, want 25", len(first))
	}
	if detail, ok := first["actor_detail"].(gin.H); !ok || detail["id"] != user.ID {
		t.Fatalf("actor_detail = %v", first["actor_detail"])
	}
	if detail, ok := first["issue_detail"].(gin.H); !ok || detail["id"] != issueID {
		t.Fatalf("issue_detail = %v", first["issue_detail"])
	}
	if detail, ok := first["project_detail"].(gin.H); !ok || detail["identifier"] != project.Identifier {
		t.Fatalf("project_detail = %v", first["project_detail"])
	}
	if detail, ok := first["workspace_detail"].(gin.H); !ok || detail["slug"] != slug {
		t.Fatalf("workspace_detail = %v", first["workspace_detail"])
	}
	// The issue never came through intake, so there is nothing to attach.
	if first["source_data"] != nil {
		t.Fatalf("source_data = %v, want null", first["source_data"])
	}

	// A caller who is not an active member of the project sees nothing at all.
	strangerID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	empty, err := handler.issueActivityRows(ctx, slug, project.ID, issueID, strangerID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("a non-member read %d activities", len(empty))
	}

	// The issue meta route reads the sequence id and the project identifier through the same join.
	var metaRow struct {
		SequenceID int    `gorm:"column:sequence_id"`
		Identifier string `gorm:"column:identifier"`
	}
	err = transaction.Session(&gorm.Session{}).Table("issues i").
		Select("i.sequence_id, p.identifier").
		Joins("JOIN projects p ON p.id = i.project_id").
		Where("i.id = ?", issueID).Take(&metaRow).Error
	if err != nil {
		t.Fatalf("read the issue meta: %v", err)
	}
	if metaRow.SequenceID != 1 || metaRow.Identifier != project.Identifier {
		t.Fatalf("meta = %#v, want the issue's sequence and the project's identifier", metaRow)
	}

	// The user property row was seeded when the project was created, so the read path finds it rather than creating a second one.
	var propertyRows []ProjectUserProperty
	err = transaction.Session(&gorm.Session{}).
		Where("user_id = ? AND project_id = ? AND deleted_at IS NULL", user.ID, project.ID).
		Find(&propertyRows).Error
	if err != nil {
		t.Fatal(err)
	}
	if len(propertyRows) != 1 {
		t.Fatalf("found %d user property rows, want exactly the seeded one", len(propertyRows))
	}
	serializedProperty := projectUserPropertyJSON(propertyRows[0])
	if len(serializedProperty) != 15 {
		t.Fatalf("serialized property has %d fields, want 15", len(serializedProperty))
	}
	if serializedProperty["user"] != user.ID || serializedProperty["project"] != project.ID {
		t.Fatalf("serialized property = %#v", serializedProperty)
	}

	// deleted-issues reads through the unfiltered manager, which is the only way to see a soft-deleted or archived row.
	var visible []string
	err = transaction.Session(&gorm.Session{}).Table("issues i").
		Where("i.project_id = ? AND (i.archived_at IS NOT NULL OR i.deleted_at IS NOT NULL)", project.ID).
		Order("i.created_at DESC").Pluck("i.id", &visible).Error
	if err != nil {
		t.Fatal(err)
	}
	// The child that was written archived and never unarchived is exactly what this route exists to report; the one the archive block unarchived must not be.
	reported, unarchivedReported := false, false
	for _, identifier := range visible {
		if identifier == archivedChild {
			reported = true
		}
		if identifier == archivableID {
			unarchivedReported = true
		}
	}
	if !reported {
		t.Fatalf("the archived issue is not among the %d deleted ids", len(visible))
	}
	if unarchivedReported {
		t.Fatal("an unarchived issue must not be reported as deleted")
	}

	// The unique constraints Django relies on must reject a duplicate name.
	duplicateID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	duplicate := project
	duplicate.ID = duplicateID
	duplicate.Identifier = "DUP" + suffix[len(suffix)-5:]
	if err := transaction.Create(&duplicate).Error; err == nil {
		t.Fatal("expected the Django unique name constraint to reject the duplicate project")
	}
}

// testWorkspace and testWorkspaceMember write the two rows the project routes
// depend on. They mirror the columns internal/workspace already exercises.
type testWorkspace struct {
	ID              string    `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
	CreatedByID     *string   `gorm:"column:created_by_id;type:uuid"`
	Name            string    `gorm:"column:name"`
	OwnerID         string    `gorm:"column:owner_id;type:uuid"`
	Slug            string    `gorm:"column:slug"`
	Timezone        string    `gorm:"column:timezone"`
	BackgroundColor string    `gorm:"column:background_color"`
}

func (testWorkspace) TableName() string { return "workspaces" }

type testWorkspaceMember struct {
	ID                      string         `gorm:"column:id;type:uuid;primaryKey"`
	CreatedAt               time.Time      `gorm:"column:created_at"`
	UpdatedAt               time.Time      `gorm:"column:updated_at"`
	CreatedByID             *string        `gorm:"column:created_by_id;type:uuid"`
	WorkspaceID             string         `gorm:"column:workspace_id;type:uuid"`
	MemberID                string         `gorm:"column:member_id;type:uuid"`
	Role                    int            `gorm:"column:role"`
	IsActive                bool           `gorm:"column:is_active"`
	ViewProps               auth.JSONValue `gorm:"column:view_props;type:jsonb"`
	DefaultProps            auth.JSONValue `gorm:"column:default_props;type:jsonb"`
	IssueProps              auth.JSONValue `gorm:"column:issue_props;type:jsonb"`
	GettingStartedChecklist auth.JSONValue `gorm:"column:getting_started_checklist;type:jsonb"`
	Tips                    auth.JSONValue `gorm:"column:tips;type:jsonb"`
	ExploredFeatures        auth.JSONValue `gorm:"column:explored_features;type:jsonb"`
}

func (testWorkspaceMember) TableName() string { return "workspace_members" }
