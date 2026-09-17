package auth

import (
	"encoding/json"
	"testing"
)

func TestCeleryMagicTaskUsesProtocolV2Shape(t *testing.T) {
	message, err := celeryMessage(magicLinkTaskName, []any{"user@pace.test", "magic_user@pace.test", "123456"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if message.Headers["task"] != magicLinkTaskName || message.ContentType != "application/json" || message.CorrelationId == "" {
		t.Fatalf("message metadata = %#v", message)
	}
	var body []any
	if err := json.Unmarshal(message.Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 3 {
		t.Fatalf("body = %#v", body)
	}
	arguments := body[0].([]any)
	if arguments[0] != "user@pace.test" || arguments[2] != "123456" {
		t.Fatalf("arguments = %#v", arguments)
	}
}

func TestWorkspaceCeleryTaskNamesMatchDjangoWorkers(t *testing.T) {
	tests := []struct {
		name      string
		arguments []any
	}{
		{name: workspaceSeedTaskName, arguments: []any{"workspace-id"}},
		{name: workspaceInvitationTaskName, arguments: []any{"member@pace.test", "workspace-id", "token", "https://app.pace.test", "owner@pace.test"}},
		{name: softDeleteRelatedObjectsTaskName, arguments: []any{"db", "workspace", "workspace-id", nil}},
	}
	for _, test := range tests {
		message, err := celeryMessage(test.name, test.arguments, nil)
		if err != nil {
			t.Fatal(err)
		}
		if message.Headers["task"] != test.name {
			t.Fatalf("task header = %#v, want %s", message.Headers["task"], test.name)
		}
		var body []any
		if err := json.Unmarshal(message.Body, &body); err != nil {
			t.Fatal(err)
		}
		if got := len(body[0].([]any)); got != len(test.arguments) {
			t.Fatalf("%s arguments = %d, want %d", test.name, got, len(test.arguments))
		}
	}
}
