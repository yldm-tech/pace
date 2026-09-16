package live

import (
	"encoding/json"
	"time"
)

// clientEvents maps the name a client sends to the name everybody on the page is told.
//
// A client asking to lock a page sends "lock" and every client, including the one that asked, is told "locked". A name that is not here is not a collaborative event and nothing is broadcast for it.
var clientEvents = map[string]string{
	"lock":            "locked",
	"unlock":          "unlocked",
	"archive":         "archived",
	"unarchive":       "unarchived",
	"make-public":     "made-public",
	"make-private":    "made-private",
	"delete":          "deleted",
	"move":            "moved",
	"duplicate":       "duplicated",
	"property_update": "property_updated",
	"restore":         "restored",
	"error":           "error",
}

func clientEventFor(payload string) (string, bool) {
	reply, ok := clientEvents[payload]
	return reply, ok
}

// The reasons a document is closed out from under the people editing it, and the close codes that carry them. The codes above four thousand are the service's own; a client reads them to decide whether reconnecting is worth trying.
const (
	forceCloseCriticalError    = "critical_error"
	forceCloseDocumentTooLarge = "document_too_large"

	closeCodeForceClose       = 4000
	closeCodeDocumentTooLarge = 4001
)

// forceCloseMessages are what a person is shown when their page is closed.
var forceCloseMessages = map[string]string{
	"critical_error":      "A critical error occurred. Please refresh the page.",
	"memory_leak":         "Memory limit exceeded. Please refresh the page.",
	"document_too_large":  "Content limit reached and live sync is off. Create a new page or use nested pages to continue syncing.",
	"admin_request":       "Connection closed by administrator. Please try again later.",
	"server_shutdown":     "Server is shutting down. Please reconnect in a moment.",
	"security_violation":  "Security violation detected. Connection terminated.",
	"corruption_detected": "Data corruption detected. Please refresh the page.",
}

func forceCloseMessage(reason string) string {
	if message, ok := forceCloseMessages[reason]; ok {
		return message
	}
	return "Connection closed. Please refresh the page."
}

// broadcastError tells everybody on a page that something went wrong with it, as a stateless signal the editor turns into a message on screen.
func (d *Document) broadcastError(message, kind string, connection ConnectionContext, code string, disconnect bool) {
	data := map[string]any{
		"error_message": message,
		"error_type":    kind,
		"user_id":       connection.UserID,
	}
	if code != "" {
		data["error_code"] = code
	}
	if disconnect {
		data["should_disconnect"] = true
	}
	event := map[string]any{
		"action":          "error",
		"page_id":         d.Name(),
		"descendants_ids": []string{},
		"data":            data,
		"workspace_slug":  connection.WorkspaceSlug,
		"user_id":         connection.UserID,
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	d.BroadcastStateless(string(encoded))
}

// forceClose tells everybody on a page why it is being closed, gives the message a moment to leave, and then closes them.
func (h *Hub) forceClose(document *Document, reason string, code int) {
	notice, err := json.Marshal(map[string]any{
		"type":      "force_close",
		"reason":    reason,
		"code":      code,
		"message":   forceCloseMessage(reason),
		"timestamp": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	})
	if err != nil {
		return
	}
	connections := document.currentConnections()
	for _, connection := range connections {
		connection.SendStateless(string(notice))
	}

	// The message and the close would otherwise race, and a client that is closed before it reads the message has nothing to show.
	time.Sleep(50 * time.Millisecond)

	for _, connection := range connections {
		connection.close(code, reason)
	}
	h.unload(document.Name())
}
