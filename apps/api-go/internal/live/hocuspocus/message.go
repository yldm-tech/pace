package hocuspocus

import "fmt"

// MessageType is the first value after a frame's document name.
type MessageType uint64

// The message types the protocol defines. Sync and Awareness are y-protocols'; the rest are Hocuspocus's own.
const (
	MessageSync               MessageType = 0
	MessageAwareness          MessageType = 1
	MessageAuth               MessageType = 2
	MessageQueryAwareness     MessageType = 3
	MessageSyncReply          MessageType = 4
	MessageStateless          MessageType = 5
	MessageBroadcastStateless MessageType = 6
	MessageClose              MessageType = 7
	MessageSyncStatus         MessageType = 8
)

// AuthMessageType is the value that follows an Auth message's type, saying which of the three shapes it is.
type AuthMessageType uint64

// The three authentication shapes. Token travels from the client; the other two travel back.
const (
	AuthToken            AuthMessageType = 0
	AuthPermissionDenied AuthMessageType = 1
	AuthAuthenticated    AuthMessageType = 2
)

// The close codes the protocol defines beyond the standard ones. A client reads these to decide whether reconnecting is worth trying.
const (
	CloseMessageTooBig     = 1009
	CloseResetConnection   = 4205
	CloseUnauthorized      = 4401
	CloseForbidden         = 4403
	CloseConnectionTimeout = 4408
)

// CloseReasons are the reasons that travel beside those codes.
var CloseReasons = map[int]string{
	CloseMessageTooBig:     "Message Too Big",
	CloseResetConnection:   "Reset Connection",
	CloseUnauthorized:      "Unauthorized",
	CloseForbidden:         "Forbidden",
	CloseConnectionTimeout: "Connection Timeout",
}

// The scopes an accepted connection is granted.
const (
	ScopeReadWrite = "read-write"
	ScopeReadOnly  = "readonly"
)

// Outgoing builds a frame for one document. Every frame starts with the document's name, so a client with several documents on one socket can tell them apart.
type Outgoing struct {
	encoder Encoder
}

// NewOutgoing starts a frame addressed to the named document.
func NewOutgoing(documentName string) *Outgoing {
	message := &Outgoing{}
	message.encoder.WriteVarString(documentName)
	return message
}

// Bytes is the finished frame.
func (m *Outgoing) Bytes() []byte { return m.encoder.Bytes() }

// Len is the frame's length so far, which the sync path compares against to decide whether the reply it built carries anything.
func (m *Outgoing) Len() int { return m.encoder.Len() }

// WriteType writes a bare message type, which is all a sync frame's header is before the y-protocols payload follows.
func (m *Outgoing) WriteType(messageType MessageType) *Outgoing {
	m.encoder.WriteVarUint(uint64(messageType))
	return m
}

// WriteSyncPayload appends an already-encoded y-protocols sync message.
func (m *Outgoing) WriteSyncPayload(payload []byte) *Outgoing {
	m.encoder.buffer = append(m.encoder.buffer, payload...)
	return m
}

// WriteAwarenessUpdate writes an awareness update.
func (m *Outgoing) WriteAwarenessUpdate(update []byte) *Outgoing {
	m.encoder.WriteVarUint(uint64(MessageAwareness))
	m.encoder.WriteVarUint8Array(update)
	return m
}

// WriteQueryAwareness asks the other side for the awareness it knows about.
func (m *Outgoing) WriteQueryAwareness() *Outgoing {
	m.encoder.WriteVarUint(uint64(MessageQueryAwareness))
	return m
}

// WriteAuthenticated accepts a connection, telling it whether it may write.
func (m *Outgoing) WriteAuthenticated(readOnly bool) *Outgoing {
	scope := ScopeReadWrite
	if readOnly {
		scope = ScopeReadOnly
	}
	m.encoder.WriteVarUint(uint64(MessageAuth))
	m.encoder.WriteVarUint(uint64(AuthAuthenticated))
	m.encoder.WriteVarString(scope)
	return m
}

// WritePermissionDenied refuses a connection, with a reason the client shows.
func (m *Outgoing) WritePermissionDenied(reason string) *Outgoing {
	m.encoder.WriteVarUint(uint64(MessageAuth))
	m.encoder.WriteVarUint(uint64(AuthPermissionDenied))
	m.encoder.WriteVarString(reason)
	return m
}

// WriteStateless sends a signal that is not part of the document.
func (m *Outgoing) WriteStateless(payload string) *Outgoing {
	m.encoder.WriteVarUint(uint64(MessageStateless))
	m.encoder.WriteVarString(payload)
	return m
}

// WriteBroadcastStateless asks every connection on the document to be sent a signal.
func (m *Outgoing) WriteBroadcastStateless(payload string) *Outgoing {
	m.encoder.WriteVarUint(uint64(MessageBroadcastStateless))
	m.encoder.WriteVarString(payload)
	return m
}

// WriteSyncStatus tells a client whether what it sent has been saved.
func (m *Outgoing) WriteSyncStatus(saved bool) *Outgoing {
	m.encoder.WriteVarUint(uint64(MessageSyncStatus))
	if saved {
		m.encoder.WriteVarUint(1)
		return m
	}
	m.encoder.WriteVarUint(0)
	return m
}

// Incoming is a frame a client sent, split into the document it addresses and what it says.
type Incoming struct {
	DocumentName string
	Type         MessageType
	decoder      *Decoder
}

// ParseIncoming reads a frame's document name and message type. The rest is left for the type's own reader.
func ParseIncoming(frame []byte) (*Incoming, error) {
	decoder := NewDecoder(frame)
	name, err := decoder.ReadVarString()
	if err != nil {
		return nil, fmt.Errorf("hocuspocus: read document name: %w", err)
	}
	messageType, err := decoder.ReadVarUint()
	if err != nil {
		return nil, fmt.Errorf("hocuspocus: read message type: %w", err)
	}
	return &Incoming{DocumentName: name, Type: MessageType(messageType), decoder: decoder}, nil
}

// Payload is the rest of the frame, which is what a sync message hands to the y-protocols reader.
func (m *Incoming) Payload() []byte { return m.decoder.Remaining() }

// Decoder is the reader positioned after the message type, for the types that read structured values.
func (m *Incoming) Decoder() *Decoder { return m.decoder }

// ReadAwarenessUpdate reads an awareness message's update.
func (m *Incoming) ReadAwarenessUpdate() ([]byte, error) {
	return m.decoder.ReadVarUint8Array()
}

// ReadStateless reads a stateless message's payload.
func (m *Incoming) ReadStateless() (string, error) {
	return m.decoder.ReadVarString()
}

// ReadAuthToken reads the token out of an authentication message, refusing any other authentication shape: the other two only ever travel from the server.
func (m *Incoming) ReadAuthToken() (string, error) {
	subType, err := m.decoder.ReadVarUint()
	if err != nil {
		return "", err
	}
	if AuthMessageType(subType) != AuthToken {
		return "", fmt.Errorf("hocuspocus: authentication message is not a token")
	}
	return m.decoder.ReadVarString()
}
