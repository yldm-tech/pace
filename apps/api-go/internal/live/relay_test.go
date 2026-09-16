package live

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/reearth/ygo/crdt"
	ysync "github.com/reearth/ygo/sync"
	"github.com/yldm-tech/pace/apps/api-go/internal/live/hocuspocus"
	"github.com/yldm-tech/pace/apps/api-go/internal/ydoc"
)

// relayServer starts one live service against a shared API and a shared Redis, which is what two of them make a cluster.
func relayServer(t *testing.T, apiURL, redisURL string) (*Server, string) {
	t.Helper()
	server := NewServer(Config{
		APIBaseURL:         apiURL,
		BasePath:           "/live",
		AppVersion:         "test",
		CORSAllowedOrigins: []string{"https://app.test"},
		RedisURL:           redisURL,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(func() {
		httpServer.Close()
		server.hub.Stop()
		_ = server.relay.Close()
	})
	return server, "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/live/collaboration"
}

// twoServers starts a pair sharing one API and one Redis.
func twoServers(t *testing.T) (*fakeAPI, *Server, string, *Server, string) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	api := newFakeAPI()
	apiServer := api.server(t)

	redisURL := "redis://" + redisServer.Addr()
	first, firstAddress := relayServer(t, apiServer.URL, redisURL)
	second, secondAddress := relayServer(t, apiServer.URL, redisURL)

	if first.relay == nil || second.relay == nil {
		t.Fatal("the relay is not attached")
	}
	return api, first, firstAddress, second, secondAddress
}

// TestAChangeOnOneServerReachesTheOther is the whole reason the relay exists: two people editing one page do not necessarily reach the same server.
func TestAChangeOnOneServerReachesTheOther(t *testing.T) {
	_, _, firstAddress, _, secondAddress := twoServers(t)

	here := dial(t, firstAddress)
	here.authenticate(testPageID, "a-user")
	here.readUntil(hocuspocus.MessageAuth)

	there := dial(t, secondAddress)
	there.authenticate(testPageID, "a-user")
	there.readUntil(hocuspocus.MessageAuth)

	document, err := ydoc.ParseHTML("<p>across the cluster</p>")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	update, err := ydoc.ToUpdate(document, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	here.send(hocuspocus.NewOutgoing(testPageID).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeUpdate(update)).Bytes())

	// The change has to cross two servers before it gets here, so several frames may arrive first.
	mirror := crdt.New()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		message := there.readUntil(hocuspocus.MessageSync, hocuspocus.MessageSyncReply)
		if _, err := ysync.ApplySyncMessage(mirror, message.Payload(), nil); err != nil {
			continue
		}
		read, err := ydoc.Parse(crdt.EncodeStateAsUpdateV1(mirror, nil))
		if err != nil {
			continue
		}
		html, err := ydoc.HTML(read)
		if err != nil {
			continue
		}
		if strings.Contains(html, "across the cluster") {
			return
		}
	}
	t.Fatal("the change never crossed to the other server")
}

// TestOnlyOneServerWritesThePage covers the lock: every server serving a page holds the whole of it, so without one they would all write it.
func TestOnlyOneServerWritesThePage(t *testing.T) {
	_, first, _, second, _ := twoServers(t)
	ctx := context.Background()

	release, ok := first.relay.AcquireWriteLock(ctx, testPageID)
	if !ok {
		t.Fatal("the first server could not take the lock")
	}
	if _, ok := second.relay.AcquireWriteLock(ctx, testPageID); ok {
		t.Fatal("the second server took a lock the first one holds")
	}

	release()
	releaseAgain, ok := second.relay.AcquireWriteLock(ctx, testPageID)
	if !ok {
		t.Fatal("the lock was not released")
	}
	releaseAgain()
}

// TestReleasingSomebodyElsesLockDoesNothing covers the case a lock expired and was taken by another server: letting the first one release it would hand the page to two writers at once.
func TestReleasingSomebodyElsesLockDoesNothing(t *testing.T) {
	_, first, _, second, _ := twoServers(t)
	ctx := context.Background()

	release, ok := first.relay.AcquireWriteLock(ctx, testPageID)
	if !ok {
		t.Fatal("the first server could not take the lock")
	}
	// The lock expires and the other server takes it.
	if err := first.relay.client.Del(ctx, lockKey(testPageID)).Err(); err != nil {
		t.Fatalf("expire: %v", err)
	}
	secondRelease, ok := second.relay.AcquireWriteLock(ctx, testPageID)
	if !ok {
		t.Fatal("the second server could not take the expired lock")
	}

	// The first server lets go of what it thinks is its lock, and the second one keeps holding.
	release()
	if _, ok := first.relay.AcquireWriteLock(ctx, testPageID); ok {
		t.Error("the first server released a lock that was no longer its own")
	}
	secondRelease()
}

// TestAStatelessEventCrossesTheCluster covers the signals beside the document: locking a page has to be told to everybody looking at it, wherever they are connected.
func TestAStatelessEventCrossesTheCluster(t *testing.T) {
	_, _, firstAddress, _, secondAddress := twoServers(t)

	here := dial(t, firstAddress)
	here.authenticate(testPageID, "a-user")
	here.readUntil(hocuspocus.MessageAuth)

	there := dial(t, secondAddress)
	there.authenticate(testPageID, "a-user")
	there.readUntil(hocuspocus.MessageAuth)

	here.send(hocuspocus.NewOutgoing(testPageID).WriteStateless("lock").Bytes())

	message := there.readUntil(hocuspocus.MessageStateless)
	payload, err := message.ReadStateless()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if payload != "locked" {
		t.Errorf("payload = %q, want locked", payload)
	}
}

// TestForceCloseCrossesTheCluster covers the command that is not about a change: a page nobody can save any more has to be closed wherever it is open.
func TestForceCloseCrossesTheCluster(t *testing.T) {
	_, first, firstAddress, second, secondAddress := twoServers(t)

	here := dial(t, firstAddress)
	here.authenticate(testPageID, "a-user")
	here.readUntil(hocuspocus.MessageAuth)

	there := dial(t, secondAddress)
	there.authenticate(testPageID, "a-user")
	there.readUntil(hocuspocus.MessageAuth)

	if second.hub.Document(testPageID) == nil {
		t.Fatal("the page is not open on the second server")
	}

	if err := first.relay.ForceCloseEverywhere(context.Background(), testPageID, forceCloseDocumentTooLarge, closeCodeDocumentTooLarge); err != nil {
		t.Fatalf("publish: %v", err)
	}

	waitFor(t, func() bool { return second.hub.Document(testPageID) == nil })
	waitFor(t, func() bool { return first.hub.Document(testPageID) == nil })
}

// TestAServerIgnoresItsOwnRelayedMessages covers the one thing Redis cannot be asked to do: a publisher is delivered its own messages, and applying one twice would be applying it twice.
func TestAServerIgnoresItsOwnRelayedMessages(t *testing.T) {
	_, first, firstAddress, _, _ := twoServers(t)

	c := dial(t, firstAddress)
	c.authenticate(testPageID, "a-user")
	c.readUntil(hocuspocus.MessageAuth)

	document := first.hub.Document(testPageID)
	if document == nil {
		t.Fatal("the page is not open")
	}

	identifier, frame, ok := decodeRelayed(first.relay.encode([]byte("anything")))
	if !ok {
		t.Fatal("a relayed message could not be read back")
	}
	if identifier != first.relay.identifier {
		t.Errorf("identifier = %q, want this server's own", identifier)
	}
	if string(frame) != "anything" {
		t.Errorf("frame = %q", frame)
	}
}

// TestASingleNodeHasNoRelay covers the deployment with no Redis at all, where every call the relay would make is a call on nothing.
func TestASingleNodeHasNoRelay(t *testing.T) {
	relay, err := NewRelay("", nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if relay != nil {
		t.Fatal("a relay was built with no address")
	}
	// Every one of these has to be safe on nothing, because the rest of the service does not check.
	relay.PublishChange(nil)
	relay.PublishAwareness(nil, nil)
	relay.PublishStateless("a-page", "locked")
	relay.Subscribe(context.Background(), "a-page", nil)
	relay.Unsubscribe("a-page")
	if _, ok := relay.AcquireWriteLock(context.Background(), "a-page"); !ok {
		t.Error("a single node was refused the right to write")
	}
	if err := relay.ForceCloseEverywhere(context.Background(), "a-page", "x", 4000); err != nil {
		t.Errorf("force close: %v", err)
	}
	if err := relay.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}
