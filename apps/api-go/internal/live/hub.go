package live

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/yldm-tech/pace/apps/api-go/internal/ydoc"
)

// storeDebounce is how long a change waits before the page is saved, and storeMaxDebounce how long a page can go unsaved while somebody keeps typing. They are the service's own settings rather than the library's defaults, and because the two are equal a page being edited continuously is saved on a fixed ten second cadence.
const (
	storeDebounce    = 10 * time.Second
	storeMaxDebounce = 10 * time.Second
)

// Hub holds every document open on this server.
type Hub struct {
	api    *APIClient
	logger *slog.Logger

	mu        sync.Mutex
	documents map[string]*Document
	// loading holds the documents being read, so that two people opening a page at the same moment wait on one read rather than starting two.
	loading map[string]chan struct{}

	saves *debouncer
}

// NewHub builds the registry.
func NewHub(api *APIClient, logger *slog.Logger) *Hub {
	return &Hub{
		api:       api,
		logger:    logger,
		documents: map[string]*Document{},
		loading:   map[string]chan struct{}{},
		saves:     newDebouncer(),
	}
}

// Stop cancels every pending save. Whatever was waiting is lost, which is why a caller that wants the pages written should close the connections first and let the last one out save.
func (h *Hub) Stop() { h.saves.Stop() }

// Document returns the document open under a name, or nil.
func (h *Hub) Document(name string) *Document {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.documents[name]
}

// Open returns the document for a page, reading it if this is the first connection to it.
func (h *Hub) Open(ctx context.Context, name string, connection ConnectionContext) (*Document, error) {
	for {
		h.mu.Lock()
		if document, ok := h.documents[name]; ok {
			h.mu.Unlock()
			return document, nil
		}
		if wait, ok := h.loading[name]; ok {
			h.mu.Unlock()
			<-wait
			continue
		}
		wait := make(chan struct{})
		h.loading[name] = wait
		h.mu.Unlock()

		document, err := h.load(ctx, name, connection)

		h.mu.Lock()
		delete(h.loading, name)
		if err == nil {
			h.documents[name] = document
		}
		h.mu.Unlock()
		close(wait)

		if err != nil {
			return nil, err
		}
		return document, nil
	}
}

// load reads a page into a document.
//
// A page that has a Yjs update is that update. A page that has none — every page created through the API rather than through the editor — is built from its HTML and its title, and the result is written straight back, so the next reader finds an update rather than doing this again.
func (h *Hub) load(ctx context.Context, name string, connection ConnectionContext) (*Document, error) {
	service, err := NewPageService(h.api, connection)
	if err != nil {
		return nil, err
	}

	binary, err := service.FetchDescriptionBinary(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("fetch the page: %w", err)
	}

	document := newDocument(name)
	if len(binary) > 0 {
		if err := crdt.ApplyUpdateV1(document.doc, binary, nil); err != nil {
			return nil, fmt.Errorf("read the page: %w", err)
		}
		h.finishLoading(document)
		return document, nil
	}

	page, err := service.FetchDetails(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("fetch the page details: %w", err)
	}
	html := page.DescriptionHTML
	if html == "" {
		html = "<p></p>"
	}
	built, err := ydoc.ParseHTML(html)
	if err != nil {
		return nil, fmt.Errorf("read the page html: %w", err)
	}
	title := ydoc.TitleDocument(page.Name)
	update, err := ydoc.ToUpdate(built, &title)
	if err != nil {
		return nil, fmt.Errorf("build the page: %w", err)
	}
	if err := crdt.ApplyUpdateV1(document.doc, update, nil); err != nil {
		return nil, fmt.Errorf("apply the built page: %w", err)
	}

	// Written back straight away rather than waiting for a change, so that a page converted from its HTML is converted once.
	if payload, err := documentPayload(update); err != nil {
		h.logger.Error("could not render the page just built from its html", "page", name, "error", err)
	} else if err := service.UpdateDescriptionBinary(ctx, name, payload); err != nil {
		h.logger.Error("could not save the page just built from its html", "page", name, "error", err)
	}

	h.finishLoading(document)
	return document, nil
}

// finishLoading opens the document for business: from here every change is passed to the other connections and queues a save.
//
// Nothing observes a document before this, which is what keeps a page that could not be read from being written back empty: a failed read never reaches here and the document is thrown away rather than registered.
func (h *Hub) finishLoading(document *Document) {
	document.observeUpdates(func(_ []byte, origin any) {
		// A change that did not come from a connection came from this server, and this server is not responsible for saving it.
		connection, ok := origin.(*Connection)
		if !ok {
			return
		}
		h.scheduleSave(document, connection.context)
	})
}

// scheduleSave puts the page in the queue to be written, replacing whatever was queued for it.
func (h *Hub) scheduleSave(document *Document, connection ConnectionContext) {
	h.saves.Debounce(document.Name(), func() {
		h.save(document, connection)
	}, storeDebounce, storeMaxDebounce)
}

// Release is what the last connection off a page does.
//
// A page with a change waiting is written now rather than left on a timer, and the write unloads it afterwards. A page with nothing waiting has nothing to write, so it is simply let go — otherwise a page somebody opened and did not touch would sit in memory until the process ended.
func (h *Hub) Release(name string) {
	if h.saves.IsDebounced(name) {
		h.saves.ExecuteNow(name)
		return
	}
	h.unload(name)
}

// save writes a page's three description columns.
//
// A failure is told to the people editing rather than only logged, because the alternative is somebody typing into a page that is no longer being saved. A page that has grown too large for the API to accept is a special case: there is no way forward, so every connection is closed and the document is unloaded.
func (h *Hub) save(document *Document, connection ConnectionContext) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	service, err := NewPageService(h.api, connection)
	if err != nil {
		h.logger.Error("could not build the page service to save with", "page", document.Name(), "error", err)
		return
	}

	payload, err := documentPayload(crdt.EncodeStateAsUpdateV1(document.Doc(), nil))
	if err != nil {
		h.logger.Error("could not render the page", "page", document.Name(), "error", err)
		return
	}

	if err := service.UpdateDescriptionBinary(ctx, document.Name(), payload); err != nil {
		h.reportSaveFailure(document, connection, err)
		return
	}

	// Nobody is left on the page and it has just been written, so it can go.
	if document.ConnectionCount() == 0 {
		h.unload(document.Name())
	}
}

func (h *Hub) reportSaveFailure(document *Document, connection ConnectionContext, err error) {
	var apiError *APIError
	tooLarge := errors.As(err, &apiError) && apiError.StatusCode == http.StatusRequestEntityTooLarge

	message := "Unable to save the page. Please try again."
	code := ""
	if tooLarge {
		message = "Document is too large to save. Please reduce the content size."
		code = "content_too_large"
	}
	h.logger.Error("could not save the page", "page", document.Name(), "error", err)
	document.broadcastError(message, "store", connection, code, tooLarge)

	if !tooLarge {
		return
	}
	// There is no way forward for a page the API will not take, so the editors are disconnected and the document is let go rather than left collecting changes nothing will ever save.
	h.forceClose(document, forceCloseDocumentTooLarge, closeCodeDocumentTooLarge)
}

// documentPayload renders an update into the three columns a page keeps.
func documentPayload(update []byte) (DocumentPayload, error) {
	document, err := ydoc.Parse(update)
	if err != nil {
		return DocumentPayload{}, err
	}
	html, err := ydoc.HTML(document)
	if err != nil {
		return DocumentPayload{}, err
	}
	encoded, err := document.JSON()
	if err != nil {
		return DocumentPayload{}, err
	}
	return DocumentPayload{
		DescriptionBinary: base64.StdEncoding.EncodeToString(update),
		DescriptionHTML:   html,
		DescriptionJSON:   json.RawMessage(encoded),
	}, nil
}

// unload takes a document out of memory. The next person to open the page reads it again.
func (h *Hub) unload(name string) {
	h.mu.Lock()
	document, ok := h.documents[name]
	if ok {
		delete(h.documents, name)
	}
	h.mu.Unlock()
	if ok {
		document.doc.Destroy()
	}
}
