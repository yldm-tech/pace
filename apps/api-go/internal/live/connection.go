package live

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	ysync "github.com/reearth/ygo/sync"
	"github.com/yldm-tech/pace/apps/api-go/internal/live/hocuspocus"
)

// idleTimeout is how long a socket that has said nothing is given before it is closed, and how often a connection is pinged once it is established.
const idleTimeout = 30 * time.Second

// writeTimeout bounds one write, so a client that has stopped reading cannot hold a goroutine forever.
const writeTimeout = 10 * time.Second

// Connection is one client editing one document.
//
// A socket may carry several documents — the protocol puts a name in front of every frame — so a socket has one of these per document, and the messages for a document that has not authenticated yet are held until it has.
type Connection struct {
	socket   *websocket.Conn
	document *Document
	context  ConnectionContext
	logger   *slog.Logger

	// send serialises writes. A websocket allows one writer at a time, and broadcasts arrive from whichever connection caused them.
	send chan []byte

	mu     sync.Mutex
	closed bool
}

func newConnection(socket *websocket.Conn, document *Document, connectionContext ConnectionContext, logger *slog.Logger) *Connection {
	connection := &Connection{
		socket:   socket,
		document: document,
		context:  connectionContext,
		logger:   logger,
		send:     make(chan []byte, 256),
	}
	document.addConnection(connection)
	return connection
}

// send queues a frame. A client too slow to keep up is closed rather than allowed to grow the queue without limit.
func (c *Connection) sendFrame(frame []byte) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	select {
	case c.send <- frame:
	default:
		c.logger.Warn("closing a connection that stopped reading", "document", c.document.Name())
		c.close(websocket.CloseTryAgainLater, "too slow")
	}
}

// SendStateless sends a signal that is not part of the document.
func (c *Connection) SendStateless(payload string) {
	c.sendFrame(hocuspocus.NewOutgoing(c.document.Name()).WriteStateless(payload).Bytes())
}

// close takes the connection off its document and shuts the socket, once.
func (c *Connection) close(code int, reason string) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.send)
	c.mu.Unlock()

	if removed := c.document.removeConnection(c); len(removed) > 0 {
		c.document.removeAwarenessFor(removed)
	}
	deadline := time.Now().Add(time.Second)
	_ = c.socket.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), deadline)
	_ = c.socket.Close()
}

// writeLoop is the only place the socket is written to, so that the one-writer rule holds without a lock around the socket itself.
func (c *Connection) writeLoop() {
	ping := time.NewTicker(idleTimeout)
	defer ping.Stop()

	for {
		select {
		case frame, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.socket.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.socket.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				c.close(websocket.CloseAbnormalClosure, "write failed")
				return
			}
		case <-ping.C:
			_ = c.socket.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.socket.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.close(websocket.CloseAbnormalClosure, "ping failed")
				return
			}
		}
	}
}

// handleMessage dispatches one frame against the document the connection is on.
func (c *Connection) handleMessage(ctx context.Context, message *hocuspocus.Incoming) error {
	switch message.Type {
	case hocuspocus.MessageSync, hocuspocus.MessageSyncReply:
		return c.handleSync(ctx, message)

	case hocuspocus.MessageAwareness:
		update, err := message.ReadAwarenessUpdate()
		if err != nil {
			return err
		}
		return c.handleAwareness(update)

	case hocuspocus.MessageQueryAwareness:
		c.sendFrame(hocuspocus.NewOutgoing(c.document.Name()).WriteAwarenessUpdate(c.document.awareness.EncodeUpdate(nil)).Bytes())
		return nil

	case hocuspocus.MessageStateless:
		payload, err := message.ReadStateless()
		if err != nil {
			return err
		}
		if reply, ok := clientEventFor(payload); ok {
			c.document.BroadcastStateless(reply)
		}
		return nil

	case hocuspocus.MessageBroadcastStateless:
		payload, err := message.ReadStateless()
		if err != nil {
			return err
		}
		c.document.BroadcastStateless(payload)
		return nil

	case hocuspocus.MessageClose:
		c.close(websocket.CloseNormalClosure, "provider_initiated")
		return nil

	case hocuspocus.MessageAuth:
		// A connection that is already established has no business authenticating again.
		c.logger.Warn("an authentication message arrived on a connection that is already authenticated", "document", c.document.Name())
		return nil

	default:
		c.logger.Warn("a message of an unknown type was ignored", "type", message.Type, "document", c.document.Name())
		return nil
	}
}

// handleSync answers a synchronisation message.
//
// A first step is answered with a second step and then a first step of our own, which is how the two sides end up knowing what each other has. A second step or an update is applied and acknowledged, and the acknowledgement is what a client waits for before it stops showing the page as unsaved.
func (c *Connection) handleSync(ctx context.Context, message *hocuspocus.Incoming) error {
	payload := message.Payload()
	messageType, _, err := ysync.ReadSyncMessage(payload)
	if err != nil {
		return err
	}

	switch messageType {
	case ysync.MsgSyncStep1:
		reply, err := ysync.EncodeSyncStep2(c.document.Doc(), payload)
		if err != nil {
			return err
		}
		// The reply carries the first step back, unless this message was itself a reply — otherwise the two sides would answer each other forever.
		if message.Type == hocuspocus.MessageSyncReply {
			c.sendFrame(hocuspocus.NewOutgoing(c.document.Name()).WriteType(hocuspocus.MessageSync).WriteSyncPayload(reply).Bytes())
			return nil
		}
		c.sendFrame(hocuspocus.NewOutgoing(c.document.Name()).WriteType(hocuspocus.MessageSync).WriteSyncPayload(reply).Bytes())
		c.sendFrame(hocuspocus.NewOutgoing(c.document.Name()).WriteType(hocuspocus.MessageSyncReply).WriteSyncPayload(ysync.EncodeSyncStep1(c.document.Doc())).Bytes())
		return nil

	case ysync.MsgSyncStep2, ysync.MsgUpdate:
		if _, err := ysync.ApplySyncMessage(c.document.Doc(), payload, c); err != nil {
			c.sendFrame(hocuspocus.NewOutgoing(c.document.Name()).WriteSyncStatus(false).Bytes())
			return err
		}
		c.sendFrame(hocuspocus.NewOutgoing(c.document.Name()).WriteSyncStatus(true).Bytes())
		return nil

	default:
		return nil
	}
}

// handleAwareness applies somebody's cursor and passes it on to everybody else.
func (c *Connection) handleAwareness(update []byte) error {
	ids, err := awarenessClients(update)
	if err != nil {
		return err
	}
	c.document.claimClients(c, ids)
	if err := c.document.awareness.ApplyUpdate(update, c); err != nil {
		return err
	}
	c.document.broadcast(hocuspocus.NewOutgoing(c.document.Name()).WriteAwarenessUpdate(update).Bytes(), c)
	return nil
}

// awarenessClients reads the client ids an awareness update speaks for, so that they can be taken away again when the connection goes.
func awarenessClients(update []byte) ([]uint64, error) {
	decoder := hocuspocus.NewDecoder(update)
	count, err := decoder.ReadVarUint()
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, count)
	for i := uint64(0); i < count; i++ {
		id, err := decoder.ReadVarUint()
		if err != nil {
			return nil, err
		}
		if _, err := decoder.ReadVarUint(); err != nil {
			return nil, err
		}
		if _, err := decoder.ReadVarString(); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// broadcastUpdate passes a change to everybody on the document except whoever made it, which is the other half of applying it.
func (d *Document) broadcastUpdate(update []byte, origin any) {
	from, _ := origin.(*Connection)
	d.broadcast(hocuspocus.NewOutgoing(d.name).WriteType(hocuspocus.MessageSync).WriteSyncPayload(ysync.EncodeUpdate(update)).Bytes(), from)
}

// observeUpdates wires the document so that every change is passed on and remembered for saving.
func (d *Document) observeUpdates(onUpdate func(update []byte, origin any)) {
	d.doc.OnUpdate(func(update []byte, origin any) {
		d.broadcastUpdate(update, origin)
		onUpdate(update, origin)
	})
}
