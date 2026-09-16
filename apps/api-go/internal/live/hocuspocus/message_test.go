package hocuspocus

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const corpusName = "11111111-1111-4111-8111-111111111111"

type frameCase struct {
	Name  string `json:"name"`
	Frame string `json:"frame"`
}

func loadFrames(t *testing.T) map[string][]byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/frames.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var cases []frameCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	frames := make(map[string][]byte, len(cases))
	for _, testCase := range cases {
		decoded, err := base64.StdEncoding.DecodeString(testCase.Frame)
		if err != nil {
			t.Fatalf("decode %s: %v", testCase.Name, err)
		}
		frames[testCase.Name] = decoded
	}
	return frames
}

// TestOutgoingMatchesHocuspocus builds every frame the server can send and compares it to what the real server's encoder produced.
func TestOutgoingMatchesHocuspocus(t *testing.T) {
	frames := loadFrames(t)

	// The sync payloads are taken out of the recorded frames rather than rebuilt, because what follows a sync header is y-protocols' encoding and this package does not produce it.
	firstSyncStep := payloadAfterHeader(t, frames["first sync step"], corpusName)
	update := payloadAfterHeader(t, frames["update"], corpusName)

	built := map[string][]byte{
		"sync":                     NewOutgoing(corpusName).WriteType(MessageSync).Bytes(),
		"sync reply":               NewOutgoing(corpusName).WriteType(MessageSyncReply).Bytes(),
		"query awareness":          NewOutgoing(corpusName).WriteQueryAwareness().Bytes(),
		"authenticated read write": NewOutgoing(corpusName).WriteAuthenticated(false).Bytes(),
		"authenticated readonly":   NewOutgoing(corpusName).WriteAuthenticated(true).Bytes(),
		"permission denied":        NewOutgoing(corpusName).WritePermissionDenied("Authentication unsuccessful").Bytes(),
		"stateless":                NewOutgoing(corpusName).WriteStateless(`{"action":"error"}`).Bytes(),
		"stateless with unicode":   NewOutgoing(corpusName).WriteStateless("héllo ☃ 🎉").Bytes(),
		"broadcast stateless":      NewOutgoing(corpusName).WriteBroadcastStateless("locked").Bytes(),
		"sync status saved":        NewOutgoing(corpusName).WriteSyncStatus(true).Bytes(),
		"sync status unsaved":      NewOutgoing(corpusName).WriteSyncStatus(false).Bytes(),
		"first sync step":          NewOutgoing(corpusName).WriteType(MessageSync).WriteSyncPayload(firstSyncStep).Bytes(),
		"update":                   NewOutgoing(corpusName).WriteType(MessageSync).WriteSyncPayload(update).Bytes(),
		"long document name":       NewOutgoing(strings.Repeat("d", 300)).WriteStateless("x").Bytes(),
		"unicode document name":    NewOutgoing("página ☃").WriteStateless("x").Bytes(),
		"empty stateless payload":  NewOutgoing(corpusName).WriteStateless("").Bytes(),
	}

	for name, want := range frames {
		got, ok := built[name]
		if !ok {
			t.Errorf("the corpus holds %q and nothing here builds it", name)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s\n go: %x\nwant: %x", name, got, want)
		}
	}
	for name := range built {
		if _, ok := frames[name]; !ok {
			t.Errorf("%q is built here and is not in the corpus", name)
		}
	}
}

// payloadAfterHeader strips a recorded frame's document name and message type, leaving the y-protocols payload.
func payloadAfterHeader(t *testing.T, frame []byte, name string) []byte {
	t.Helper()
	message, err := ParseIncoming(frame)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if message.DocumentName != name {
		t.Fatalf("document name = %q, want %q", message.DocumentName, name)
	}
	return message.Payload()
}

// TestParseIncomingReadsWhatHocuspocusWrote walks the recorded frames back apart, which is the direction the server reads them in.
func TestParseIncomingReadsWhatHocuspocusWrote(t *testing.T) {
	frames := loadFrames(t)

	for _, expected := range []struct {
		frame        string
		documentName string
		messageType  MessageType
	}{
		{"sync", corpusName, MessageSync},
		{"sync reply", corpusName, MessageSyncReply},
		{"query awareness", corpusName, MessageQueryAwareness},
		{"authenticated read write", corpusName, MessageAuth},
		{"stateless", corpusName, MessageStateless},
		{"broadcast stateless", corpusName, MessageBroadcastStateless},
		{"sync status saved", corpusName, MessageSyncStatus},
		{"long document name", strings.Repeat("d", 300), MessageStateless},
		{"unicode document name", "página ☃", MessageStateless},
	} {
		message, err := ParseIncoming(frames[expected.frame])
		if err != nil {
			t.Fatalf("%s: parse: %v", expected.frame, err)
		}
		if message.DocumentName != expected.documentName {
			t.Errorf("%s: document name = %q, want %q", expected.frame, message.DocumentName, expected.documentName)
		}
		if message.Type != expected.messageType {
			t.Errorf("%s: type = %d, want %d", expected.frame, message.Type, expected.messageType)
		}
	}

	stateless, err := ParseIncoming(frames["stateless with unicode"])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	payload, err := stateless.ReadStateless()
	if err != nil {
		t.Fatalf("read stateless: %v", err)
	}
	if payload != "héllo ☃ 🎉" {
		t.Errorf("payload = %q", payload)
	}

	empty, err := ParseIncoming(frames["empty stateless payload"])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if payload, err := empty.ReadStateless(); err != nil || payload != "" {
		t.Errorf("payload = %q, err = %v, want an empty payload and no error", payload, err)
	}
}

// TestAuthTokenRoundTrip covers the one authentication shape that travels from the client, and the refusal of the two that do not.
func TestAuthTokenRoundTrip(t *testing.T) {
	var encoder Encoder
	encoder.WriteVarString("a-document")
	encoder.WriteVarUint(uint64(MessageAuth))
	encoder.WriteVarUint(uint64(AuthToken))
	encoder.WriteVarString(`{"id":"someone","cookie":"session=abc"}`)

	message, err := ParseIncoming(encoder.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	token, err := message.ReadAuthToken()
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if token != `{"id":"someone","cookie":"session=abc"}` {
		t.Errorf("token = %q", token)
	}

	denied, err := ParseIncoming(NewOutgoing("a-document").WritePermissionDenied("no").Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := denied.ReadAuthToken(); err == nil {
		t.Error("a permission denied message was read as a token")
	}
}

// TestVarUintRoundTrip covers the boundaries where a number grows another byte.
func TestVarUintRoundTrip(t *testing.T) {
	for _, value := range []uint64{0, 1, 127, 128, 255, 256, 16383, 16384, 1 << 20, 1 << 32, (1 << 63) - 1} {
		var encoder Encoder
		encoder.WriteVarUint(value)
		got, err := NewDecoder(encoder.Bytes()).ReadVarUint()
		if err != nil {
			t.Fatalf("%d: %v", value, err)
		}
		if got != value {
			t.Errorf("round trip of %d gave %d", value, got)
		}
	}
}

func TestDecoderRejectsTruncatedFrames(t *testing.T) {
	full := NewOutgoing(corpusName).WriteStateless("a payload").Bytes()
	for cut := 0; cut < len(full); cut++ {
		message, err := ParseIncoming(full[:cut])
		if err != nil {
			continue
		}
		if _, err := message.ReadStateless(); err == nil {
			t.Errorf("a frame cut to %d bytes was read without error", cut)
		}
	}
}

func TestDecoderRejectsInvalidUTF8(t *testing.T) {
	var encoder Encoder
	encoder.WriteVarUint8Array([]byte{0xff, 0xfe})
	if _, err := NewDecoder(encoder.Bytes()).ReadVarString(); err == nil {
		t.Error("a string that is not utf-8 was read without error")
	}
}

func TestCloseReasonsCoverEveryCode(t *testing.T) {
	for _, code := range []int{CloseMessageTooBig, CloseResetConnection, CloseUnauthorized, CloseForbidden, CloseConnectionTimeout} {
		if CloseReasons[code] == "" {
			t.Errorf("close code %d has no reason", code)
		}
	}
	if got, want := fmt.Sprint(CloseReasons[CloseUnauthorized]), "Unauthorized"; got != want {
		t.Errorf("reason for 4401 = %q, want %q", got, want)
	}
}
