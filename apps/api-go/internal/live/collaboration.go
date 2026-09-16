package live

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/yldm-tech/pace/apps/api-go/internal/live/hocuspocus"
)

// maxFrameSize caps one inbound frame. A page large enough to exceed it is a page the API would refuse anyway.
const maxFrameSize = 16 << 20

// collaboration serves the websocket every open page is connected to.
//
// A client connects once per page and the page's id travels in the frames rather than in the url, so the document a connection is for is not known until its first message arrives.
func (s *Server) collaboration(c *gin.Context) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		// The origin is checked by the CORS middleware in front of this, and a websocket upgrade carries the same Origin header.
		CheckOrigin: func(r *http.Request) bool { return s.originAllowed(r.Header.Get("Origin")) },
	}
	socket, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.logger.Warn("could not upgrade a collaboration connection", "error", err)
		return
	}
	socket.SetReadLimit(maxFrameSize)

	client := &clientSocket{
		server:     s,
		socket:     socket,
		headers:    c.Request.Header.Clone(),
		parameters: c.Request.URL.Query(),
		documents:  map[string]*Connection{},
		queued:     map[string][][]byte{},
		logger:     s.logger,
	}
	client.run(c.Request.Context())
}

// originAllowed is the same list the CORS middleware works from. A connection from anywhere else is refused before the upgrade rather than after it.
func (s *Server) originAllowed(origin string) bool {
	for _, allowed := range s.config.CORSAllowedOrigins {
		if allowed != "" && allowed == origin {
			return true
		}
	}
	// A client that sends no origin at all is not a browser, and the session cookie it would need cannot be attached by one.
	return origin == ""
}

// clientSocket is one websocket, which may carry several documents.
type clientSocket struct {
	server     *Server
	socket     *websocket.Conn
	headers    http.Header
	parameters url.Values
	logger     *slog.Logger

	mu sync.Mutex
	// documents holds the connections that have authenticated, and queued the messages for the ones that have not.
	documents map[string]*Connection
	queued    map[string][][]byte
	// establishing marks a document whose authentication is under way, so a second authentication message for it is not acted on twice.
	establishing map[string]bool
}

func (s *clientSocket) run(ctx context.Context) {
	defer s.shutdown()

	// A socket that says nothing at all is closed. Once a document is established the connection's own ping keeps it alive.
	_ = s.socket.SetReadDeadline(time.Now().Add(idleTimeout))
	s.socket.SetPongHandler(func(string) error {
		return s.socket.SetReadDeadline(time.Now().Add(idleTimeout * 2))
	})

	for {
		_, frame, err := s.socket.ReadMessage()
		if err != nil {
			return
		}
		_ = s.socket.SetReadDeadline(time.Now().Add(idleTimeout * 2))

		message, err := hocuspocus.ParseIncoming(frame)
		if err != nil {
			s.logger.Warn("could not read a collaboration frame", "error", err)
			s.closeSocket(hocuspocus.CloseUnauthorized, hocuspocus.CloseReasons[hocuspocus.CloseUnauthorized])
			return
		}

		s.mu.Lock()
		connection, established := s.documents[message.DocumentName]
		s.mu.Unlock()

		if established {
			if err := connection.handleMessage(ctx, message); err != nil {
				s.logger.Warn("could not handle a collaboration message", "document", message.DocumentName, "error", err)
			}
			continue
		}

		s.handleBeforeEstablished(ctx, message, frame)
	}
}

// handleBeforeEstablished deals with a frame for a document that has not authenticated. Only the authentication message is acted on; everything else waits.
func (s *clientSocket) handleBeforeEstablished(ctx context.Context, message *hocuspocus.Incoming, frame []byte) {
	if message.Type != hocuspocus.MessageAuth {
		s.queue(message.DocumentName, frame)
		return
	}

	s.mu.Lock()
	if s.establishing == nil {
		s.establishing = map[string]bool{}
	}
	if s.establishing[message.DocumentName] {
		s.mu.Unlock()
		return
	}
	s.establishing[message.DocumentName] = true
	s.mu.Unlock()

	token, err := message.ReadAuthToken()
	if err != nil {
		s.logger.Warn("could not read an authentication message", "error", err)
		s.closeSocket(hocuspocus.CloseUnauthorized, hocuspocus.CloseReasons[hocuspocus.CloseUnauthorized])
		return
	}
	s.establish(ctx, message.DocumentName, token)
}

// establish authenticates a document's connection and opens it.
//
// A refusal is sent back and the socket is left open, which is what the client expects: it shows the failure and decides for itself whether to try again.
func (s *clientSocket) establish(ctx context.Context, name, token string) {
	connectionContext, err := buildContext(token, s.headers, s.parameters)
	if err == nil {
		_, err = s.server.authenticate(ctx, connectionContext)
	}
	if err != nil {
		s.logger.Warn("refused a collaboration connection", "document", name, "error", err)
		s.write(hocuspocus.NewOutgoing(name).WritePermissionDenied("permission-denied").Bytes())
		return
	}

	document, err := s.server.hub.Open(ctx, name, connectionContext)
	if err != nil {
		s.logger.Error("could not open a page", "page", name, "error", err)
		s.write(hocuspocus.NewOutgoing(name).WritePermissionDenied("permission-denied").Bytes())
		return
	}

	s.write(hocuspocus.NewOutgoing(name).WriteAuthenticated(false).Bytes())

	connection := newConnection(s.socket, document, connectionContext, s.server.hub, s.logger)
	go connection.writeLoop()

	s.mu.Lock()
	s.documents[name] = connection
	queued := s.queued[name]
	delete(s.queued, name)
	s.mu.Unlock()

	// Whoever is already on the page is told about the people on it, which is what puts their cursors on screen.
	if update := document.awareness.EncodeUpdate(nil); len(update) > 1 {
		connection.sendFrame(hocuspocus.NewOutgoing(name).WriteAwarenessUpdate(update).Bytes())
	}

	for _, frame := range queued {
		message, err := hocuspocus.ParseIncoming(frame)
		if err != nil {
			continue
		}
		if err := connection.handleMessage(ctx, message); err != nil {
			s.logger.Warn("could not handle a queued collaboration message", "document", name, "error", err)
		}
	}
}

func (s *clientSocket) queue(name string, frame []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queued[name] = append(s.queued[name], append([]byte(nil), frame...))
}

// write sends a frame for a document that has no connection yet, which is only ever an authentication answer.
func (s *clientSocket) write(frame []byte) {
	_ = s.socket.SetWriteDeadline(time.Now().Add(writeTimeout))
	_ = s.socket.WriteMessage(websocket.BinaryMessage, frame)
}

func (s *clientSocket) closeSocket(code int, reason string) {
	_ = s.socket.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
	_ = s.socket.Close()
}

// shutdown closes every document this socket carried. The last connection off a page saves it straight away rather than leaving it on a timer.
func (s *clientSocket) shutdown() {
	s.mu.Lock()
	connections := make([]*Connection, 0, len(s.documents))
	for _, connection := range s.documents {
		connections = append(connections, connection)
	}
	s.documents = map[string]*Connection{}
	s.mu.Unlock()

	for _, connection := range connections {
		name := connection.document.Name()
		connection.close(websocket.CloseNormalClosure, "")
		if connection.document.ConnectionCount() == 0 {
			s.server.hub.Release(name)
		}
	}
	_ = s.socket.Close()
}
