package auth

import (
	"encoding/json"
	"testing"
)

func TestCeleryMagicTaskUsesProtocolV2Shape(t *testing.T) {
	message, err := celeryMessage(magicLinkTaskName, []any{"user@pace.test", "magic_user@pace.test", "123456"})
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
