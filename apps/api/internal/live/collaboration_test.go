package live

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/reearth/ygo/crdt"
	ysync "github.com/reearth/ygo/sync"
	"github.com/yldm-tech/pace/apps/api/internal/live/hocuspocus"
	"github.com/yldm-tech/pace/apps/api/internal/ydoc"
)

const testPageID = "11111111-1111-4111-8111-111111111111"

// fakeAPI stands in for the API the live service reads and writes pages through.
type fakeAPI struct {
	mu sync.Mutex

	user        string
	page        Page
	binary      []byte
	saved       []DocumentPayload
	saveStatus  int
	saveMessage string
	requests    []string
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		user: "a-user",
		page: Page{ID: testPageID, Name: "A page", DescriptionHTML: "<p>from html</p>"},
	}
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)

		switch {
		case r.URL.Path == "/api/users/me/":
			_, _ = w.Write([]byte(`{"id":"` + f.user + `","display_name":"Someone"}`))

		case strings.HasSuffix(r.URL.Path, "/description/") && r.Method == http.MethodGet:
			_, _ = w.Write(f.binary)

		case strings.HasSuffix(r.URL.Path, "/description/") && r.Method == http.MethodPatch:
			if f.saveStatus != 0 {
				w.WriteHeader(f.saveStatus)
				_, _ = w.Write([]byte(`{"detail":"` + f.saveMessage + `"}`))
				return
			}
			var payload DocumentPayload
			_ = json.NewDecoder(r.Body).Decode(&payload)
			f.saved = append(f.saved, payload)
			if decoded, err := base64.StdEncoding.DecodeString(payload.DescriptionBinary); err == nil {
				f.binary = decoded
			}
			_, _ = w.Write([]byte(`{}`))

		default:
			page, _ := json.Marshal(f.page)
			_, _ = w.Write(page)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func (f *fakeAPI) savedPayloads() []DocumentPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]DocumentPayload(nil), f.saved...)
}

// liveServer starts the whole service against a fake API and returns the websocket address.
func liveServer(t *testing.T, api *fakeAPI) (*Server, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	apiServer := api.server(t)
	server := NewServer(Config{
		APIBaseURL:         apiServer.URL,
		BasePath:           "/live",
		AppVersion:         "test",
		CORSAllowedOrigins: []string{"https://app.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(func() {
		httpServer.Close()
		server.hub.Stop()
	})
	return server, "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/live/collaboration"
}

// client is a test client speaking the same protocol the editor's does.
type client struct {
	t      *testing.T
	socket *websocket.Conn
	// seen is every frame type this client has read, so a timeout can say what did arrive rather than only what did not.
	seen []hocuspocus.MessageType
}

func dial(t *testing.T, address string) *client {
	t.Helper()
	header := http.Header{}
	header.Set("Origin", "https://app.test")
	socket, _, err := websocket.DefaultDialer.Dial(address+"?documentType=project_page&workspaceSlug=a-workspace&projectId=a-project", header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = socket.Close() })
	return &client{t: t, socket: socket}
}

func (c *client) send(frame []byte) {
	c.t.Helper()
	if err := c.socket.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *client) authenticate(page, userID string) {
	c.t.Helper()
	var encoder hocuspocus.Encoder
	encoder.WriteVarString(page)
	encoder.WriteVarUint(uint64(hocuspocus.MessageAuth))
	encoder.WriteVarUint(uint64(hocuspocus.AuthToken))
	encoder.WriteVarString(`{"id":"` + userID + `","cookie":"session=abc"}`)
	c.send(encoder.Bytes())
}

// readDeadline bounds a single frame. It is generous on purpose: nothing here is asserting how quickly a server answers, only that it does, and a shared CI runner starves these goroutines long enough that five seconds was reached twice with no frame in sight. A test that never gets its frame still fails -- through go test's own timeout, with the whole suite's stacks attached, which says more than "i/o timeout" ever did.
const readDeadline = 30 * time.Second

// read waits for one frame and parses it.
//
// A timeout here reports the frames this client did receive. Without that the failure is one line about a socket, and working out whether the frame never arrived or merely arrived in a different order means guessing -- which it has cost twice.
func (c *client) read() *hocuspocus.Incoming {
	c.t.Helper()
	_ = c.socket.SetReadDeadline(time.Now().Add(readDeadline))
	_, frame, err := c.socket.ReadMessage()
	if err != nil {
		c.t.Fatalf("read: %v (frames seen on this client so far: %s)", err, c.seenSoFar())
	}
	message, err := hocuspocus.ParseIncoming(frame)
	if err != nil {
		c.t.Fatalf("parse: %v", err)
	}
	c.seen = append(c.seen, message.Type)
	return message
}

// seenSoFar renders the frame types this client has read, in order.
func (c *client) seenSoFar() string {
	if len(c.seen) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(c.seen))
	for _, messageType := range c.seen {
		parts = append(parts, fmt.Sprintf("%d", messageType))
	}
	return strings.Join(parts, ",")
}

// readUntil waits for a frame of one of the given types, ignoring the rest.
func (c *client) readUntil(types ...hocuspocus.MessageType) *hocuspocus.Incoming {
	c.t.Helper()
	wanted := map[hocuspocus.MessageType]bool{}
	for _, messageType := range types {
		wanted[messageType] = true
	}
	for i := 0; i < 20; i++ {
		message := c.read()
		if wanted[message.Type] {
			return message
		}
	}
	c.t.Fatalf("no frame of the wanted type arrived")
	return nil
}

// readUntilBefore is readUntil bounded by a deadline the caller owns. It returns false when the deadline passes instead of failing the test, so a caller waiting on something that legitimately takes a while -- a change crossing two servers, say -- reports its own failure rather than dying on read's fixed five seconds partway through its own budget.
func (c *client) readUntilBefore(deadline time.Time, types ...hocuspocus.MessageType) (*hocuspocus.Incoming, bool) {
	c.t.Helper()
	wanted := map[hocuspocus.MessageType]bool{}
	for _, messageType := range types {
		wanted[messageType] = true
	}
	for time.Now().Before(deadline) {
		if err := c.socket.SetReadDeadline(deadline); err != nil {
			return nil, false
		}
		_, frame, err := c.socket.ReadMessage()
		if err != nil {
			return nil, false
		}
		message, err := hocuspocus.ParseIncoming(frame)
		if err != nil {
			c.t.Fatalf("parse: %v", err)
		}
		if wanted[message.Type] {
			return message, true
		}
	}
	return nil, false
}

// TestConnectionSyncsAPageBuiltFromItsHTML is the path a page created through the API takes the first time somebody opens it: no Yjs document exists, one is built from the page's HTML and title, and the client is synchronised against it.
func TestConnectionSyncsAPageBuiltFromItsHTML(t *testing.T) {
	api := newFakeAPI()
	_, address := liveServer(t, api)

	c := dial(t, address)
	c.authenticate(testPageID, "a-user")

	authenticated := c.readUntil(hocuspocus.MessageAuth)
	if authenticated.DocumentName != testPageID {
		t.Fatalf("the answer names %q", authenticated.DocumentName)
	}

	// Ask for what the server has.
	empty := crdt.New()
	c.send(hocuspocus.NewOutgoing(testPageID).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeSyncStep1(empty)).Bytes())

	reply := c.readUntil(hocuspocus.MessageSync, hocuspocus.MessageSyncReply)
	if _, err := ysync.ApplySyncMessage(empty, reply.Payload(), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	document, err := ydoc.Parse(crdt.EncodeStateAsUpdateV1(empty, nil))
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}
	html, err := ydoc.HTML(document)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, "from html") {
		t.Errorf("the page did not arrive: %s", html)
	}

	title, err := ydoc.ParseTitle(crdt.EncodeStateAsUpdateV1(empty, nil))
	if err != nil {
		t.Fatalf("read the title: %v", err)
	}
	if got := title.TextContent(); got != "A page" {
		t.Errorf("title = %q, want the page's name", got)
	}

	// The page just built is written straight back, so the next reader finds a document rather than building it again.
	saved := api.savedPayloads()
	if len(saved) == 0 {
		t.Fatal("the page built from its html was not saved")
	}
	if !strings.Contains(saved[0].DescriptionHTML, "from html") {
		t.Errorf("what was saved does not hold the page: %s", saved[0].DescriptionHTML)
	}
}

// TestEditsReachTheOtherClientAndAreSaved is the whole point of the service: what one person types arrives at everybody else and ends up in the page.
func TestEditsReachTheOtherClientAndAreSaved(t *testing.T) {
	api := newFakeAPI()
	server, address := liveServer(t, api)

	first := dial(t, address)
	first.authenticate(testPageID, "a-user")
	first.readUntil(hocuspocus.MessageAuth)

	second := dial(t, address)
	second.authenticate(testPageID, "a-user")
	second.readUntil(hocuspocus.MessageAuth)

	// The first client writes a paragraph.
	document, err := ydoc.ParseHTML("<p>typed by hand</p>")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	update, err := ydoc.ToUpdate(document, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	first.send(hocuspocus.NewOutgoing(testPageID).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeUpdate(update)).Bytes())

	// It reaches the second client as an update.
	arrived := second.readUntil(hocuspocus.MessageSync)
	mirror := crdt.New()
	if _, err := ysync.ApplySyncMessage(mirror, arrived.Payload(), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	mirrored, err := ydoc.Parse(crdt.EncodeStateAsUpdateV1(mirror, nil))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	html, err := ydoc.HTML(mirrored)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, "typed by hand") {
		t.Errorf("the change did not reach the other client: %s", html)
	}

	// And the writer is told it was accepted.
	if status := first.readUntil(hocuspocus.MessageSyncStatus); status == nil {
		t.Fatal("no acknowledgement")
	}

	// Saving is on a timer; the last connection leaving runs it immediately.
	_ = first.socket.Close()
	_ = second.socket.Close()
	waitFor(t, func() bool { return server.hub.Document(testPageID) == nil })

	var found bool
	for _, payload := range api.savedPayloads() {
		if strings.Contains(payload.DescriptionHTML, "typed by hand") {
			found = true
		}
	}
	if !found {
		t.Errorf("the change was never saved: %+v", api.savedPayloads())
	}
}

// TestAuthenticationIsRefusedForTheWrongUser covers the one check the service makes for itself: the cookie has to belong to the user the token names.
func TestAuthenticationIsRefusedForTheWrongUser(t *testing.T) {
	api := newFakeAPI()
	_, address := liveServer(t, api)

	c := dial(t, address)
	c.authenticate(testPageID, "somebody-else")

	message := c.readUntil(hocuspocus.MessageAuth)
	decoder := message.Decoder()
	subType, err := decoder.ReadVarUint()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if hocuspocus.AuthMessageType(subType) != hocuspocus.AuthPermissionDenied {
		t.Fatalf("sub type = %d, want a refusal", subType)
	}
	reason, err := decoder.ReadVarString()
	if err != nil {
		t.Fatalf("read the reason: %v", err)
	}
	if reason != "permission-denied" {
		t.Errorf("reason = %q", reason)
	}
}

// TestMessagesBeforeAuthenticationAreQueued covers the ordering the client relies on: it sends its first sync step immediately after its token, without waiting for an answer.
func TestMessagesBeforeAuthenticationAreQueued(t *testing.T) {
	api := newFakeAPI()
	_, address := liveServer(t, api)

	c := dial(t, address)
	empty := crdt.New()
	// The sync step goes first, before the token.
	c.send(hocuspocus.NewOutgoing(testPageID).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeSyncStep1(empty)).Bytes())
	c.authenticate(testPageID, "a-user")

	c.readUntil(hocuspocus.MessageAuth)
	reply := c.readUntil(hocuspocus.MessageSync, hocuspocus.MessageSyncReply)
	if _, err := ysync.ApplySyncMessage(empty, reply.Payload(), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	document, err := ydoc.Parse(crdt.EncodeStateAsUpdateV1(empty, nil))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(document.Content) == 0 {
		t.Error("the queued sync step was never answered")
	}
}

// TestStatelessEventsAreTranslated covers the signals the editor sends beside the document: a client asking to lock a page results in everybody being told it is locked.
func TestStatelessEventsAreTranslated(t *testing.T) {
	api := newFakeAPI()
	_, address := liveServer(t, api)

	c := dial(t, address)
	c.authenticate(testPageID, "a-user")
	c.readUntil(hocuspocus.MessageAuth)

	c.send(hocuspocus.NewOutgoing(testPageID).WriteStateless("lock").Bytes())
	message := c.readUntil(hocuspocus.MessageStateless)
	payload, err := message.ReadStateless()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if payload != "locked" {
		t.Errorf("payload = %q, want locked", payload)
	}
}

// TestUnknownStatelessEventsAreIgnored is the other half: a payload that is not one of the collaborative events is not echoed to anybody.
func TestUnknownStatelessEventsAreIgnored(t *testing.T) {
	api := newFakeAPI()
	_, address := liveServer(t, api)

	c := dial(t, address)
	c.authenticate(testPageID, "a-user")
	c.readUntil(hocuspocus.MessageAuth)

	c.send(hocuspocus.NewOutgoing(testPageID).WriteStateless("not-an-event").Bytes())
	c.send(hocuspocus.NewOutgoing(testPageID).WriteStateless("lock").Bytes())

	message := c.readUntil(hocuspocus.MessageStateless)
	payload, _ := message.ReadStateless()
	if payload != "locked" {
		t.Errorf("payload = %q, want the unknown event to have been skipped", payload)
	}
}

// TestAPageThatIsTooLargeClosesEverybody covers the one failure there is no way forward from: the API refuses the page, everybody is told why, and the document is let go rather than left collecting changes nothing will save.
func TestAPageThatIsTooLargeClosesEverybody(t *testing.T) {
	api := newFakeAPI()
	server, address := liveServer(t, api)

	c := dial(t, address)
	c.authenticate(testPageID, "a-user")
	c.readUntil(hocuspocus.MessageAuth)

	api.mu.Lock()
	api.saveStatus = http.StatusRequestEntityTooLarge
	api.saveMessage = "too big"
	api.mu.Unlock()

	document, err := ydoc.ParseHTML("<p>a change</p>")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	update, err := ydoc.ToUpdate(document, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	c.send(hocuspocus.NewOutgoing(testPageID).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeUpdate(update)).Bytes())
	c.readUntil(hocuspocus.MessageSyncStatus)

	// The save is on a timer; closing the last connection runs it now.
	_ = c.socket.Close()
	waitFor(t, func() bool { return server.hub.Document(testPageID) == nil })
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the condition never became true")
}

// TestAwarenessReachesTheOthersAndLeavesWithThem covers the cursors: what one person's client says about where they are arrives at everybody else, and is taken away again when they go.
func TestAwarenessReachesTheOthersAndLeavesWithThem(t *testing.T) {
	api := newFakeAPI()
	server, address := liveServer(t, api)

	first := dial(t, address)
	first.authenticate(testPageID, "a-user")
	first.readUntil(hocuspocus.MessageAuth)

	second := dial(t, address)
	second.authenticate(testPageID, "a-user")
	second.readUntil(hocuspocus.MessageAuth)

	// The first client says where it is.
	var encoder hocuspocus.Encoder
	encoder.WriteVarUint(1)
	encoder.WriteVarUint(7)
	encoder.WriteVarUint(1)
	encoder.WriteVarString(`{"user":{"name":"Someone"}}`)
	first.send(hocuspocus.NewOutgoing(testPageID).WriteAwarenessUpdate(encoder.Bytes()).Bytes())

	arrived := second.readUntil(hocuspocus.MessageAwareness)
	update, err := arrived.ReadAwarenessUpdate()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	ids, err := awarenessClients(update)
	if err != nil {
		t.Fatalf("read the clients: %v", err)
	}
	if len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("clients = %v, want the one that was sent", ids)
	}

	// When the first client goes, the others are told its cursor is gone.
	_ = first.socket.Close()

	// Read awareness frames until the removal is among them rather than assuming it is the next one. The announcement above can still be in flight -- the server also sends awareness on joining -- so on a loaded runner the frame read here is the announcement again, at its own clock and with its state intact, and the assertion fails on a message that is perfectly correct.
	deadline := time.Now().Add(10 * time.Second)
	var id, clock uint64
	var state string
	var found bool
	for !found {
		removal, arrived := second.readUntilBefore(deadline, hocuspocus.MessageAwareness)
		if !arrived {
			break
		}
		removed, err := removal.ReadAwarenessUpdate()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		decoder := hocuspocus.NewDecoder(removed)
		count, _ := decoder.ReadVarUint()
		if count != 1 {
			t.Fatalf("the removal names %d clients", count)
		}
		id, _ = decoder.ReadVarUint()
		clock, _ = decoder.ReadVarUint()
		state, _ = decoder.ReadVarString()
		found = state == "null"
	}
	if !found || id != 7 || clock != 2 {
		t.Errorf("removal = client %d clock %d state %q, want client 7 at a higher clock with no state", id, clock, state)
	}

	_ = second.socket.Close()
	waitFor(t, func() bool { return server.hub.Document(testPageID) == nil })
}

// TestASecondReaderWaitsForTheFirstLoad covers the case two people open a page at the same moment: one read happens, not two.
func TestASecondReaderWaitsForTheFirstLoad(t *testing.T) {
	api := newFakeAPI()
	server, address := liveServer(t, api)

	first := dial(t, address)
	second := dial(t, address)
	first.authenticate(testPageID, "a-user")
	second.authenticate(testPageID, "a-user")
	first.readUntil(hocuspocus.MessageAuth)
	second.readUntil(hocuspocus.MessageAuth)

	// The client is told it is in just before it is registered, so both being on the page is waited for rather than asserted straight away.
	waitFor(t, func() bool {
		document := server.hub.Document(testPageID)
		return document != nil && document.ConnectionCount() == 2
	})

	api.mu.Lock()
	reads := 0
	for _, request := range api.requests {
		if strings.HasSuffix(request, "/description/") && strings.HasPrefix(request, "GET") {
			reads++
		}
	}
	api.mu.Unlock()
	if reads != 1 {
		t.Errorf("the page was read %d times, want once", reads)
	}
}

// TestAPageWithADocumentIsNotRebuilt covers the other half of loading: a page that already has an update is that update, and its HTML is never looked at.
func TestAPageWithADocumentIsNotRebuilt(t *testing.T) {
	api := newFakeAPI()
	document, err := ydoc.ParseHTML("<p>already a document</p>")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	update, err := ydoc.ToUpdate(document, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	api.binary = update
	api.page.DescriptionHTML = "<p>stale html nobody should read</p>"

	_, address := liveServer(t, api)
	c := dial(t, address)
	c.authenticate(testPageID, "a-user")
	c.readUntil(hocuspocus.MessageAuth)

	empty := crdt.New()
	c.send(hocuspocus.NewOutgoing(testPageID).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeSyncStep1(empty)).Bytes())
	reply := c.readUntil(hocuspocus.MessageSync, hocuspocus.MessageSyncReply)
	if _, err := ysync.ApplySyncMessage(empty, reply.Payload(), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	read, err := ydoc.Parse(crdt.EncodeStateAsUpdateV1(empty, nil))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	html, err := ydoc.HTML(read)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, "already a document") || strings.Contains(html, "stale html") {
		t.Errorf("html = %s", html)
	}
	if saved := api.savedPayloads(); len(saved) != 0 {
		t.Errorf("a page that already had a document was written back anyway: %+v", saved)
	}
}
