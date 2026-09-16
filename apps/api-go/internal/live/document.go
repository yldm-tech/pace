package live

import (
	"sync"

	"github.com/reearth/ygo/awareness"
	"github.com/reearth/ygo/crdt"
	"github.com/yldm-tech/pace/apps/api-go/internal/live/hocuspocus"
)

// Document is one page held in memory while somebody has it open: the CRDT itself, who is looking at it, and every connection editing it.
type Document struct {
	name      string
	doc       *crdt.Doc
	awareness *awareness.Awareness

	mu sync.RWMutex
	// connections is keyed by the connection itself, because a page open twice in one browser is two connections on one socket.
	connections map[*Connection]struct{}
	// clients records which awareness client ids each connection has claimed, so they can be removed when it goes.
	clients map[*Connection]map[uint64]struct{}
}

func newDocument(name string) *Document {
	document := &Document{
		name:        name,
		doc:         crdt.New(),
		connections: map[*Connection]struct{}{},
		clients:     map[*Connection]map[uint64]struct{}{},
	}
	document.awareness = awareness.New(uint64(document.doc.ClientID()))
	return document
}

// Name is the page id the document belongs to.
func (d *Document) Name() string { return d.name }

// Doc is the CRDT, for the callers that read or write it.
func (d *Document) Doc() *crdt.Doc { return d.doc }

func (d *Document) addConnection(connection *Connection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connections[connection] = struct{}{}
	d.clients[connection] = map[uint64]struct{}{}
}

// removeConnection drops a connection and takes its awareness with it, so the people still on the page stop seeing a cursor that has gone.
func (d *Document) removeConnection(connection *Connection) []uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.connections[connection]; !ok {
		return nil
	}
	delete(d.connections, connection)
	claimed := d.clients[connection]
	delete(d.clients, connection)
	ids := make([]uint64, 0, len(claimed))
	for id := range claimed {
		ids = append(ids, id)
	}
	return ids
}

func (d *Document) hasConnection(connection *Connection) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.connections[connection]
	return ok
}

// claimClients records the awareness client ids a connection has spoken for.
func (d *Document) claimClients(connection *Connection, ids []uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	claimed, ok := d.clients[connection]
	if !ok {
		return
	}
	for _, id := range ids {
		claimed[id] = struct{}{}
	}
}

// ConnectionCount is how many people have the page open here. It does not count the people on another server.
func (d *Document) ConnectionCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.connections)
}

func (d *Document) currentConnections() []*Connection {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*Connection, 0, len(d.connections))
	for connection := range d.connections {
		out = append(out, connection)
	}
	return out
}

// broadcast sends a frame to everybody on the document except the connection it came from, which already has it.
func (d *Document) broadcast(frame []byte, except *Connection) {
	for _, connection := range d.currentConnections() {
		if connection == except {
			continue
		}
		connection.sendFrame(frame)
	}
}

// BroadcastStateless sends a signal to everybody on the document, including whoever asked for it.
func (d *Document) BroadcastStateless(payload string) {
	for _, connection := range d.currentConnections() {
		connection.SendStateless(payload)
	}
}

// removeAwarenessFor tells everybody that a connection's cursors are gone, by applying an update that raises each client's clock and blanks its state — which is how y-protocols says somebody left.
func (d *Document) removeAwarenessFor(ids []uint64) {
	if len(ids) == 0 {
		return
	}
	var encoder hocuspocus.Encoder
	encoder.WriteVarUint(uint64(len(ids)))
	for _, id := range ids {
		clock := uint64(0)
		if meta, ok := d.awareness.Meta(id); ok {
			clock = meta.Clock
		}
		encoder.WriteVarUint(id)
		encoder.WriteVarUint(clock + 1)
		encoder.WriteVarString("null")
	}
	update := encoder.Bytes()
	if err := d.awareness.ApplyUpdate(update, nil); err != nil {
		return
	}
	d.broadcast(hocuspocus.NewOutgoing(d.name).WriteAwarenessUpdate(update).Bytes(), nil)
}
