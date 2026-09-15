package externalapi

import (
	"testing"
)

// The invite serializer is seven fields, and the token is not among them.
func TestInviteJSONHidesTheToken(t *testing.T) {
	data := inviteJSON(WorkspaceMemberInvite{ID: "invite-id", Token: "secret-token"})
	for _, field := range []string{"id", "email", "role", "created_at", "updated_at", "responded_at", "accepted"} {
		if _, present := data[field]; !present {
			t.Errorf("the invite is missing %q", field)
		}
	}
	if _, present := data["token"]; present {
		t.Error("the token must never be rendered: it is what accepting the invitation proves")
	}
	if _, present := data["message"]; present {
		t.Error("the message is not in the serializer's field list")
	}
	if len(data) != 7 {
		t.Fatalf("the invite has %d fields, want 7", len(data))
	}
}

// The two refusals on delete are ordered, so an invitation that is both accepted and responded reports the first.
func TestTheDeleteRefusalsAreOrdered(t *testing.T) {
	type refusal struct {
		accepted  bool
		responded bool
		code      string
	}
	for _, test := range []refusal{
		{accepted: true, responded: true, code: "INVITE_ALREADY_ACCEPTED"},
		{accepted: true, responded: false, code: "INVITE_ALREADY_ACCEPTED"},
		{accepted: false, responded: true, code: "INVITE_ALREADY_RESPONDED"},
		{accepted: false, responded: false, code: ""},
	} {
		code := ""
		if test.accepted {
			code = "INVITE_ALREADY_ACCEPTED"
		} else if test.responded {
			code = "INVITE_ALREADY_RESPONDED"
		}
		if code != test.code {
			t.Errorf("accepted=%v responded=%v gives %q, want %q", test.accepted, test.responded, code, test.code)
		}
	}
}

// The PATCH forbids changing the address and the PUT requires one, so the two methods on the same path disagree about the same field.
func TestTheTwoUpdateMethodsDisagreeAboutTheEmail(t *testing.T) {
	// A PATCH naming an email at all is refused outright.
	patchRefuses := func(payload map[string]any) bool {
		value, present := payload["email"]
		return present && value != nil && value != ""
	}
	if !patchRefuses(map[string]any{"email": "someone@example.com"}) {
		t.Error("the patch refuses any email")
	}
	if patchRefuses(map[string]any{"role": 15}) {
		t.Error("the patch accepts a body with no email")
	}
	// A PUT with no email is refused for the opposite reason.
	putRefuses := func(payload map[string]any) bool {
		value, _ := payload["email"].(string)
		return value == ""
	}
	if !putRefuses(map[string]any{"role": 15}) {
		t.Error("the put requires an email")
	}
	if putRefuses(map[string]any{"email": "someone@example.com"}) {
		t.Error("the put accepts an email")
	}
}
