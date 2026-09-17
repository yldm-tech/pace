package worker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The payload keeps the key order it is documented in, and the two halves that came from a serializer are spliced in as they arrived rather than rebuilt.
func TestThePayloadKeepsItsOrder(t *testing.T) {
	body, err := webhookPayload(webhookDelivery{
		event: "issue", slug: "acme",
		// The nested keys are deliberately not alphabetical, so a rebuild would be visible.
		eventData: json.RawMessage(`{"name": "Hello", "id": "issue-id"}`),
		activity:  json.RawMessage(`{"field": "name", "actor": "who"}`),
	}, webhookRow{ID: "hook-id", WorkspaceID: "workspace-id"}, "create")
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(body)
	for _, pair := range [][2]string{
		{`"event"`, `"action"`}, {`"action"`, `"webhook_id"`}, {`"webhook_id"`, `"workspace_id"`},
		{`"workspace_id"`, `"workspace_slug"`}, {`"workspace_slug"`, `"data"`}, {`"data"`, `"activity"`},
	} {
		if strings.Index(rendered, pair[0]) > strings.Index(rendered, pair[1]) {
			t.Errorf("%s comes after %s in %s", pair[0], pair[1], rendered)
		}
	}
	if !strings.Contains(rendered, `{"name": "Hello", "id": "issue-id"}`) {
		t.Errorf("the event data was rebuilt: %s", rendered)
	}
	// It has to be valid json all the same.
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("the payload is not json: %v", err)
	}
	if decoded["action"] != "create" || decoded["workspace_slug"] != "acme" {
		t.Errorf("the payload decoded to %#v", decoded)
	}
}

// A half that was not sent is null rather than missing.
func TestAnAbsentHalfIsNull(t *testing.T) {
	body, err := webhookPayload(webhookDelivery{event: "issue", slug: "acme"}, webhookRow{}, "delete")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"data", "activity"} {
		value, present := decoded[key]
		if !present || value != nil {
			t.Errorf("%q is %v (present %v), want null", key, value, present)
		}
	}
}

// The signature is over exactly the bytes that are sent, which is what lets a receiver check it.
func TestTheSignatureCoversTheBody(t *testing.T) {
	body := []byte(`{"event": "issue"}`)
	signature := hmac.New(sha256.New, []byte("secret"))
	signature.Write(body)
	want := hex.EncodeToString(signature.Sum(nil))
	if len(want) != 64 {
		t.Fatalf("the signature is %d characters", len(want))
	}
}

// The http method a change was made with is named rather than sent as itself, and a method with no name is sent as it is.
func TestTheActionNames(t *testing.T) {
	for method, want := range map[string]string{
		"POST": "create", "PATCH": "update", "PUT": "update", "DELETE": "delete",
	} {
		if webhookVerbs[method] != want {
			t.Errorf("%s is named %q, want %q", method, webhookVerbs[method], want)
		}
	}
	if _, named := webhookVerbs["GET"]; named {
		t.Error("GET has a name and should not")
	}
}

// Every wait is a uniformly random stretch of up to ten minutes, because the doubling is capped at the same ten minutes it starts from.
func TestTheBackoffIsNotExponential(t *testing.T) {
	for attempt := 1; attempt <= webhookMaxRetries; attempt++ {
		for trial := 0; trial < 50; trial++ {
			delay := defaultWebhookBackoff(attempt)
			if delay < 0 || delay > webhookBackoffCeil {
				t.Fatalf("attempt %d waited %v, which is outside the window", attempt, delay)
			}
		}
	}
	if webhookBackoffCeil != 600*time.Second {
		t.Errorf("the ceiling is %v", webhookBackoffCeil)
	}
	if webhookMaxRetries != 5 {
		t.Errorf("there are %d retries", webhookMaxRetries)
	}
}

// The headers are written the way a python dict prints, which is what the log column holds today.
func TestTheHeaderRendering(t *testing.T) {
	rendered := renderHeaderMap(map[string]string{
		"Content-Type": "application/json", "User-Agent": "Autopilot",
		"X-Plane-Delivery": "d", "X-Plane-Event": "issue", "X-Plane-Signature": "s",
	})
	if !strings.HasPrefix(rendered, "{'Content-Type': 'application/json', 'User-Agent': 'Autopilot'") {
		t.Errorf("the headers render as %s", rendered)
	}
	if !strings.HasSuffix(rendered, "'X-Plane-Signature': 's'}") {
		t.Errorf("the headers render as %s", rendered)
	}
	// A webhook with no secret has no signature header at all.
	rendered = renderHeaderMap(map[string]string{"Content-Type": "application/json"})
	if strings.Contains(rendered, "Signature") {
		t.Errorf("an unsigned delivery rendered %s", rendered)
	}
}

// TestQuotesInAValueCannotBreakThePayload is the answer to CodeQL's unsafe-quoting alert on webhookPayload. The format string builds the object by hand, which is what the rule looks for, but every value spliced into it has already been through json.Marshal or arrived as a json.RawMessage, and none of the verbs sits inside quotes. A double quote in a value therefore lands escaped rather than ending a string early. This test fails the moment that stops being true.
func TestQuotesInAValueCannotBreakThePayload(t *testing.T) {
	hostile := `he said "hi", then \ left`
	body, err := webhookPayload(webhookDelivery{
		event:     hostile,
		slug:      hostile,
		eventData: json.RawMessage(`{"note": "quote \" inside"}`),
	}, webhookRow{ID: hostile, WorkspaceID: hostile}, hostile)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("a value carrying a double quote broke the payload: %v\n%s", err, body)
	}
	for _, key := range []string{"event", "action", "webhook_id", "workspace_id", "workspace_slug"} {
		if decoded[key] != hostile {
			t.Errorf("%q decoded to %#v, want the value it was given", key, decoded[key])
		}
	}
}

// TestTheBytesMatchDjangoSeparators pins the one thing about this payload that cannot be changed for tidiness. The signature in X-Plane-Signature is an HMAC over these exact bytes, and Django produced them with json.dumps, whose default separators put a space after every colon and comma. encoding/json writes neither, so marshalling a struct here -- however much cleaner it would read -- would change what every receiver verifies against.
func TestTheBytesMatchDjangoSeparators(t *testing.T) {
	body, err := webhookPayload(webhookDelivery{
		event: "issue", slug: "acme",
		eventData: json.RawMessage(`null`),
		activity:  json.RawMessage(`null`),
	}, webhookRow{ID: "hook-id", WorkspaceID: "workspace-id"}, "create")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"event": "issue", "action": "create", "webhook_id": "hook-id", "workspace_id": "workspace-id", "workspace_slug": "acme", "data": null, "activity": null}`
	if string(body) != want {
		t.Errorf("payload bytes\n got %s\nwant %s", body, want)
	}
}
