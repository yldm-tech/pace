package live

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	ysync "github.com/reearth/ygo/sync"
	"github.com/yldm-tech/pace/apps/api-go/internal/live/hocuspocus"
)

// The channel names the relay uses. A document has a channel of its own, and there is one more for the commands that are not about any particular document.
const (
	relayPrefix  = "hocuspocus"
	adminChannel = "hocuspocus:admin"
)

// lockTimeout is how long a server holds the right to write a page, and disconnectDelay how long a document's channel stays subscribed after the last local connection leaves. Both are the service's own settings.
const (
	lockTimeout     = time.Second
	disconnectDelay = time.Second
)

// Relay carries a page's changes between the servers serving it.
//
// Two people editing the same page do not necessarily reach the same server, so every change one server applies has to reach the others. The carrier is Redis, one channel per document, and the messages on it are the same frames the clients send — so a server relaying to another is doing what a client does, from the other side.
type Relay struct {
	client *redis.Client
	hub    *Hub
	logger *slog.Logger

	// identifier tells this server's own messages apart. Redis delivers a publisher its own messages and there is no way to ask it not to, so they are recognised and dropped here.
	identifier string

	mu            sync.Mutex
	subscriptions map[string]*redis.PubSub
	pending       map[string]*time.Timer

	admin   *redis.PubSub
	adminWG sync.WaitGroup
}

// NewRelay connects to Redis. An empty address means the service runs as a single node, and a nil relay is what the rest of the code then works with.
func NewRelay(address string, hub *Hub, logger *slog.Logger) (*Relay, error) {
	if address == "" {
		return nil, nil
	}
	options, err := redis.ParseURL(address)
	if err != nil {
		return nil, fmt.Errorf("live: parse the redis url: %w", err)
	}
	client := redis.NewClient(options)

	relay := &Relay{
		client:        client,
		hub:           hub,
		logger:        logger,
		identifier:    "host-" + uuid.NewString(),
		subscriptions: map[string]*redis.PubSub{},
		pending:       map[string]*time.Timer{},
	}
	relay.listenForAdminCommands()
	return relay, nil
}

// Close stops listening. A document left subscribed would keep receiving changes for a document nobody here has.
func (r *Relay) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	for name, subscription := range r.subscriptions {
		_ = subscription.Close()
		delete(r.subscriptions, name)
	}
	for name, timer := range r.pending {
		timer.Stop()
		delete(r.pending, name)
	}
	admin := r.admin
	r.mu.Unlock()

	if admin != nil {
		_ = admin.Close()
	}
	r.adminWG.Wait()
	return r.client.Close()
}

func documentChannel(name string) string { return relayPrefix + ":" + name }

func lockKey(name string) string { return documentChannel(name) + ":lock" }

// encode puts this server's name in front of a frame, so the other servers can tell whose it is and this one can drop its own.
func (r *Relay) encode(frame []byte) []byte {
	out := make([]byte, 0, 1+len(r.identifier)+len(frame))
	out = append(out, byte(len(r.identifier)))
	out = append(out, r.identifier...)
	return append(out, frame...)
}

// decode splits a relayed message back into who sent it and what they said.
func decodeRelayed(message []byte) (string, []byte, bool) {
	if len(message) == 0 {
		return "", nil, false
	}
	length := int(message[0])
	if len(message) < 1+length {
		return "", nil, false
	}
	return string(message[1 : 1+length]), message[1+length:], true
}

func (r *Relay) publish(ctx context.Context, name string, frame []byte) {
	if err := r.client.Publish(ctx, documentChannel(name), r.encode(frame)).Err(); err != nil {
		r.logger.Warn("could not relay a change", "document", name, "error", err)
	}
}

// Subscribe starts carrying a document's changes, and tells the other servers this one has it: it publishes what it holds and asks who else is on the page.
func (r *Relay) Subscribe(ctx context.Context, name string, document *Document) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if timer, ok := r.pending[name]; ok {
		timer.Stop()
		delete(r.pending, name)
	}
	if _, ok := r.subscriptions[name]; ok {
		r.mu.Unlock()
		return
	}
	subscription := r.client.Subscribe(ctx, documentChannel(name))
	r.subscriptions[name] = subscription
	r.mu.Unlock()

	// Wait for Redis to confirm the subscription before publishing anything below. Subscribe only queues the command -- Channel(), which receive calls, is what sends it, from a goroutine of its own -- so without this the two publishes race the subscription. Pub/sub keeps no backlog, so an answer that arrives before this server is listening is not late, it is gone: the page then holds whatever it had until somebody types into it again.
	if _, err := subscription.Receive(ctx); err != nil {
		r.logger.Warn("live: the relay subscription was not confirmed", "document", name, "error", err)
	}

	go r.receive(name, subscription)

	r.publish(ctx, name, hocuspocus.NewOutgoing(name).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeSyncStep1(document.Doc())).Bytes())
	r.publish(ctx, name, hocuspocus.NewOutgoing(name).WriteQueryAwareness().Bytes())
}

// Unsubscribe stops carrying a document, after a pause. The pause is what keeps a page somebody is reconnecting to from being dropped and taken up again a moment later.
func (r *Relay) Unsubscribe(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.subscriptions[name]; !ok {
		return
	}
	if timer, ok := r.pending[name]; ok {
		timer.Stop()
	}
	r.pending[name] = time.AfterFunc(disconnectDelay, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.pending, name)
		subscription, ok := r.subscriptions[name]
		if !ok {
			return
		}
		delete(r.subscriptions, name)
		_ = subscription.Close()
	})
}

// receive reads a document's channel until it is closed.
func (r *Relay) receive(name string, subscription *redis.PubSub) {
	for message := range subscription.Channel() {
		identifier, frame, ok := decodeRelayed([]byte(message.Payload))
		if !ok {
			continue
		}
		// This server's own message comes back to it, and applying it again would be applying it twice.
		if identifier == r.identifier {
			continue
		}
		r.apply(name, frame)
	}
}

// apply handles a frame another server sent.
//
// The frames are the ones the clients send, so this is the same dispatch from the other side — with one difference that matters: a change arriving this way is not this server's to save, and it answers back over Redis rather than down a socket.
func (r *Relay) apply(name string, frame []byte) {
	message, err := hocuspocus.ParseIncoming(frame)
	if err != nil {
		return
	}
	document := r.hub.Document(message.DocumentName)
	if document == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	switch message.Type {
	case hocuspocus.MessageSync, hocuspocus.MessageSyncReply:
		r.applySync(ctx, document, message)

	case hocuspocus.MessageAwareness:
		update, err := message.ReadAwarenessUpdate()
		if err != nil {
			return
		}
		if err := document.awareness.ApplyUpdate(update, relayOrigin); err != nil {
			return
		}
		document.broadcast(hocuspocus.NewOutgoing(document.Name()).WriteAwarenessUpdate(update).Bytes(), nil)

	case hocuspocus.MessageQueryAwareness:
		if update := document.awareness.EncodeUpdate(nil); len(update) > 1 {
			r.publish(ctx, name, hocuspocus.NewOutgoing(name).WriteAwarenessUpdate(update).Bytes())
		}

	case hocuspocus.MessageStateless, hocuspocus.MessageBroadcastStateless:
		payload, err := message.ReadStateless()
		if err != nil {
			return
		}
		// Passed on to the clients here without being relayed again, which is what would otherwise bounce it between the servers forever.
		for _, connection := range document.currentConnections() {
			connection.SendStateless(payload)
		}
	}
}

// relayOrigin marks a change that came from another server. The hub reads it to decide who is responsible for saving: whoever's client made the change, not whoever heard about it.
var relayOrigin = &struct{ name string }{name: "relay"}

func (r *Relay) applySync(ctx context.Context, document *Document, message *hocuspocus.Incoming) {
	payload := message.Payload()
	messageType, _, err := ysync.ReadSyncMessage(payload)
	if err != nil {
		return
	}
	switch messageType {
	case ysync.MsgSyncStep1:
		// Another server has said what it holds. Two things go back: what it is missing, and — unless this was already an answer — what this server holds, so that it can send back whatever this one is missing.
		//
		// That second half is the whole exchange. A server announcing a change publishes its own state vector, which on its own tells the others nothing about the change; it is the answer to the answer that carries it.
		if message.Type == hocuspocus.MessageSync {
			r.publish(ctx, document.Name(), hocuspocus.NewOutgoing(document.Name()).WriteType(hocuspocus.MessageSyncReply).WriteSyncPayload(ysync.EncodeSyncStep1(document.Doc())).Bytes())
		}
		reply, err := ysync.EncodeSyncStep2(document.Doc(), payload)
		if err != nil {
			return
		}
		r.publish(ctx, document.Name(), hocuspocus.NewOutgoing(document.Name()).WriteType(hocuspocus.MessageSync).WriteSyncPayload(reply).Bytes())

	case ysync.MsgSyncStep2, ysync.MsgUpdate:
		if _, err := ysync.ApplySyncMessage(document.Doc(), payload, relayOrigin); err != nil {
			r.logger.Warn("could not apply a relayed change", "document", document.Name(), "error", err)
		}
	}
}

// PublishChange tells the other servers that a document here has changed, by saying what this server now holds. Each of them answers with whatever this one is missing.
func (r *Relay) PublishChange(document *Document) {
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	r.publish(ctx, document.Name(), hocuspocus.NewOutgoing(document.Name()).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeSyncStep1(document.Doc())).Bytes())
}

// PublishAwareness tells the other servers where somebody's cursor is.
func (r *Relay) PublishAwareness(document *Document, update []byte) {
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	r.publish(ctx, document.Name(), hocuspocus.NewOutgoing(document.Name()).WriteAwarenessUpdate(update).Bytes())
}

// PublishStateless passes a signal on to the other servers.
func (r *Relay) PublishStateless(name, payload string) {
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	r.publish(ctx, name, hocuspocus.NewOutgoing(name).WriteBroadcastStateless(payload).Bytes())
}

// AcquireWriteLock asks for the right to write a page.
//
// Every server serving a page has the whole of it, so every one of them would otherwise write it, and the last write would win a race nobody needed to run. The lock is held only for the write and expires on its own, so a server that dies holding one does not stop the page being saved.
func (r *Relay) AcquireWriteLock(ctx context.Context, name string) (func(), bool) {
	if r == nil {
		return func() {}, true
	}
	token := uuid.NewString()
	acquired, err := r.client.SetNX(ctx, lockKey(name), token, lockTimeout).Result()
	if err != nil {
		r.logger.Warn("could not reach redis for the write lock", "page", name, "error", err)
		// Redis being unreachable should not stop a page being saved; the worst case is the write another server was also making.
		return func() {}, true
	}
	if !acquired {
		return nil, false
	}
	return func() {
		// Released only if it is still ours: a lock that expired and was taken by somebody else is not ours to give away.
		release := redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)
		_ = release.Run(context.Background(), r.client, []string{lockKey(name)}, token).Err()
	}, true
}

// adminCommand is a message on the channel that is not about any particular document.
type adminCommand struct {
	Command      string `json:"command"`
	DocumentID   string `json:"docId"`
	Reason       string `json:"reason"`
	Code         int    `json:"code"`
	OriginServer string `json:"originServer"`
	Timestamp    string `json:"timestamp"`
}

// listenForAdminCommands subscribes to the channel a page is closed from.
func (r *Relay) listenForAdminCommands() {
	subscription := r.client.Subscribe(context.Background(), adminChannel)
	r.mu.Lock()
	r.admin = subscription
	r.mu.Unlock()

	r.adminWG.Add(1)
	go func() {
		defer r.adminWG.Done()
		for message := range subscription.Channel() {
			var command adminCommand
			if err := json.Unmarshal([]byte(message.Payload), &command); err != nil {
				r.logger.Warn("could not read an admin command", "error", err)
				continue
			}
			if command.Command != "force_close" {
				continue
			}
			document := r.hub.Document(command.DocumentID)
			if document == nil {
				// The page is not open here, so there is nobody to close.
				continue
			}
			r.hub.forceClose(document, command.Reason, command.Code)
		}
	}()
}

// ForceCloseEverywhere asks every server to close a page, including this one.
func (r *Relay) ForceCloseEverywhere(ctx context.Context, name, reason string, code int) error {
	if r == nil {
		return nil
	}
	command, err := json.Marshal(adminCommand{
		Command:      "force_close",
		DocumentID:   name,
		Reason:       reason,
		Code:         code,
		OriginServer: r.identifier,
		Timestamp:    time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	})
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, adminChannel, command).Err()
}
