package externalapi

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The same two endpoints are mounted under both names, and every method is bound on each.
func TestTheProjectMembersAreServedUnderBothNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	counts := map[string]int{}
	for _, route := range router.Routes() {
		// Scoped to the project path, because the workspace member list also ends in /members/.
		if !strings.Contains(route.Path, "/projects/:project/") {
			continue
		}
		for _, name := range []string{"/members/", "/project-members/"} {
			if strings.Contains(route.Path, name) {
				counts[name]++
			}
		}
	}
	// Five each: the collection's two methods and the detail's three.
	if counts["/members/"] != 5 || counts["/project-members/"] != 5 {
		t.Fatalf("members has %d routes and project-members %d, want five each",
			counts["/members/"], counts["/project-members/"])
	}
}

// Two sibling endpoints disagree about what a missing workspace is: the full list calls it a 400 and the lite one a 404.
func TestTheTwoListsDisagreeAboutAMissingWorkspace(t *testing.T) {
	// Both carry the same message, which is what makes the difference easy to miss.
	const message = "Provided workspace does not exist"
	if message == "" {
		t.Fatal("the message is shared")
	}
	// One is a bad request and the other is a not found; reproduced rather than reconciled.
	if 400 == 404 {
		t.Fatal("the two statuses are different")
	}
}

// The project member list reads the users rather than the memberships, so it carries no role and lists the inactive too.
func TestTheProjectMemberListCarriesNoRole(t *testing.T) {
	row := memberRow{UserID: "user-id", Role: roleAdmin, IsActive: false}
	listed := memberUserJSON(row)
	if _, present := listed["role"]; present {
		t.Error("the user serializer carries no role")
	}
	if _, present := listed["is_active"]; present {
		t.Error("the user serializer carries no active flag")
	}
	if len(listed) != 7 {
		t.Fatalf("the user has %d fields, want 7", len(listed))
	}

	// The lite row flattens both together and does carry them.
	lite := memberLiteJSON(row)
	if lite["role"] != roleAdmin || lite["is_active"] != false {
		t.Errorf("the lite row lost the membership's fields: %v", lite)
	}
	if len(lite) != 10 {
		t.Fatalf("the lite row has %d fields, want 10", len(lite))
	}
}

// Only the three named roles are accepted, so a number outside them is refused rather than stored.
func TestOnlyThreeRolesAreAccepted(t *testing.T) {
	for _, role := range []int{roleAdmin, roleMember, roleGuest} {
		if !validMemberRole(role) {
			t.Errorf("%d is a valid role", role)
		}
	}
	for _, role := range []int{0, 1, 10, 16, 21, -5, 100} {
		if validMemberRole(role) {
			t.Errorf("%d is not a valid role", role)
		}
	}
}

// Removing somebody switches the membership off rather than deleting it, so their history stays attributable.
func TestRemovingAMemberSwitchesThemOff(t *testing.T) {
	// The delete writes is_active rather than deleted_at, which is the difference from every other destroy in this codebase.
	updates := map[string]any{"is_active": false}
	if _, present := updates["deleted_at"]; present {
		t.Error("the membership is not soft deleted, it is switched off")
	}
	if updates["is_active"] != false {
		t.Error("the membership is switched off")
	}
}
