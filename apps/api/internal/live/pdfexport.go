package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/pdfdoc"
	"github.com/yldm-tech/pace/apps/api/internal/ydoc"
)

// The limits the export works under, which are the service's own.
const (
	contentFetchTimeout = 7 * time.Second
	imageTimeout        = 8 * time.Second
	imageConcurrency    = 4
)

// pdfExportRequest is what the route is asked with.
type pdfExportRequest struct {
	PageID          string `json:"pageId"`
	WorkspaceSlug   string `json:"workspaceSlug"`
	ProjectID       string `json:"projectId"`
	Title           string `json:"title"`
	Author          string `json:"author"`
	Subject         string `json:"subject"`
	PageSize        string `json:"pageSize"`
	PageOrientation string `json:"pageOrientation"`
	FileName        string `json:"fileName"`
	NoAssets        bool   `json:"noAssets"`
}

// pdfError carries the status a failure is answered with, so that the caller can tell a page it may not read from one the service could not draw.
type pdfError struct {
	status  int
	message string
}

func (e *pdfError) Error() string { return e.message }

// exportPDF draws a page as a PDF.
//
// It is the one route here whose answer is not JSON, and the one whose output cannot be compared with the service it replaces: the original lays a page out with a flexbox engine and this flows blocks down the page. The same fonts are embedded and every measurement comes from the original's own stylesheet, so a page looks like the same page without being the same bytes.
func (s *Server) exportPDF(c *gin.Context) {
	cookie := c.GetHeader("Cookie")
	if cookie == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Authentication required"})
		return
	}

	var request pdfExportRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	if strings.TrimSpace(request.PageID) == "" || strings.TrimSpace(request.WorkspaceSlug) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	if !validPageSize(request.PageSize) || !validOrientation(request.PageOrientation) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}

	document, err := s.exportedPDF(c.Request.Context(), request, cookie)
	if err != nil {
		var failure *pdfError
		if errors.As(err, &failure) {
			s.logger.Error("could not export a page as a pdf", "page", request.PageID, "error", failure.message)
			c.JSON(failure.status, gin.H{"message": failure.message})
			return
		}
		s.logger.Error("could not export a page as a pdf", "page", request.PageID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to generate PDF"})
		return
	}

	name := outputFileName(request)
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", sanitiseFileName(name), url.PathEscape(name)))
	c.Header("Content-Length", strconv.Itoa(len(document)))
	c.Data(http.StatusOK, "application/pdf", document)
}

func validPageSize(size string) bool {
	switch size {
	case "", "A4", "A3", "A2", "LETTER", "LEGAL", "TABLOID":
		return true
	}
	return false
}

func validOrientation(orientation string) bool {
	switch orientation {
	case "", "portrait", "landscape":
		return true
	}
	return false
}

// exportedPDF reads the page, resolves what it refers to, and draws it.
func (s *Server) exportedPDF(ctx context.Context, request pdfExportRequest, cookie string) ([]byte, error) {
	service, err := NewPageService(s.api, ConnectionContext{
		Cookie:        cookie,
		DocumentType:  ProjectPage,
		WorkspaceSlug: request.WorkspaceSlug,
		ProjectID:     request.ProjectID,
	})
	if err != nil {
		// A request naming no project cannot name a page either, and upstream does not check for it: the page service throws, the throw is a defect rather than a failure, and the answer is the same five hundred any unexpected failure gets. Reproduced rather than turned into the four hundred it looks like it should be.
		return nil, &pdfError{status: http.StatusInternalServerError, message: "Failed to generate PDF"}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, contentFetchTimeout)
	defer cancel()
	binary, err := service.FetchDescriptionBinary(fetchCtx, request.PageID)
	if err != nil {
		return nil, &pdfError{status: http.StatusBadGateway, message: "Failed to fetch page content"}
	}
	if len(binary) == 0 {
		return nil, &pdfError{status: http.StatusNotFound, message: "Page content not found"}
	}

	document, err := ydoc.Parse(binary)
	if err != nil {
		return nil, &pdfError{status: http.StatusBadGateway, message: "Failed to fetch page content"}
	}

	options := pdfdoc.Options{
		Title:        request.Title,
		Author:       request.Author,
		Subject:      request.Subject,
		PageSize:     request.PageSize,
		Orientation:  request.PageOrientation,
		NoAssets:     request.NoAssets,
		UserMentions: s.mentionNames(ctx, service, request.PageID),
	}
	if request.Title == "" {
		// The title the page itself carries, when the request named none.
		if title, err := ydoc.ParseTitle(binary); err == nil {
			options.Title = title.TextContent()
		}
	}
	if !request.NoAssets {
		options.Images = s.pageImages(ctx, service, document, request)
	}

	drawn, err := pdfdoc.Render(document, options)
	if err != nil {
		return nil, &pdfError{status: http.StatusInternalServerError, message: "Failed to generate PDF"}
	}
	return drawn, nil
}

// mentionNames is who the page mentions. Failing to find out is not failing the export — the mentions are drawn as unknown instead.
func (s *Server) mentionNames(ctx context.Context, service *PageService, pageID string) map[string]string {
	fetchCtx, cancel := context.WithTimeout(ctx, contentFetchTimeout)
	defer cancel()
	mentions, err := service.FetchUserMentions(fetchCtx, pageID)
	if err != nil {
		s.logger.Warn("could not read the people a page mentions", "page", pageID, "error", err)
		return nil
	}
	names := make(map[string]string, len(mentions))
	for _, mention := range mentions {
		names[mention.ID] = mention.DisplayName
	}
	return names
}

// pageImages fetches the pictures a page holds, several at a time. One that cannot be fetched is drawn as a placeholder rather than failing the export.
func (s *Server) pageImages(ctx context.Context, service *PageService, document ydoc.Node, request pdfExportRequest) map[string][]byte {
	assets := pdfdoc.AssetIDs(document)
	if len(assets) == 0 {
		return nil
	}

	images := make(map[string][]byte, len(assets))
	var mu sync.Mutex
	var wait sync.WaitGroup
	tokens := make(chan struct{}, imageConcurrency)

	for _, asset := range assets {
		wait.Add(1)
		go func(asset string) {
			defer wait.Done()
			tokens <- struct{}{}
			defer func() { <-tokens }()

			fetchCtx, cancel := context.WithTimeout(ctx, imageTimeout)
			defer cancel()
			data, err := service.FetchAsset(fetchCtx, request.WorkspaceSlug, request.ProjectID, asset)
			if err != nil {
				s.logger.Warn("could not fetch a picture for a pdf", "asset", asset, "error", err)
				return
			}
			mu.Lock()
			images[asset] = data
			mu.Unlock()
		}(asset)
	}
	wait.Wait()
	return images
}

// outputFileName is what the download is called: what the request asked for, or the page's title, or a name of last resort.
func outputFileName(request pdfExportRequest) string {
	name := strings.TrimSpace(request.FileName)
	if name == "" {
		name = strings.TrimSpace(request.Title)
	}
	if name == "" {
		name = "page"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	return name
}

// sanitiseFileName is the name as it may appear inside the quoted part of the header: a quote, a backslash or a line ending would let the name write headers of its own, and anything outside plain ASCII has no meaning there.
func sanitiseFileName(name string) string {
	var out strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '\\' || r == '\r' || r == '\n':
		case r < 0x20 || r > 0x7e:
			out.WriteRune('_')
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// FetchAsset reads one of a page's pictures, following the redirect the API answers with rather than being followed into it with the user's session attached.
func (s *PageService) FetchAsset(ctx context.Context, workspaceSlug, projectID, assetID string) ([]byte, error) {
	path := fmt.Sprintf("/api/assets/v2/workspaces/%s/%s/", workspaceSlug, assetID)
	if projectID != "" {
		path = fmt.Sprintf("/api/assets/v2/workspaces/%s/projects/%s/%s/", workspaceSlug, projectID, assetID)
	}
	response, payload, err := s.client.request(ctx, http.MethodGet, path, s.cookie, nil, map[string]string{"disposition": "inline"})
	if err != nil {
		return nil, err
	}
	// The asset routes answer with a redirect into storage, and the picture is behind it.
	if response.StatusCode == http.StatusFound || response.StatusCode == http.StatusMovedPermanently {
		location := response.Header.Get("Location")
		if location == "" {
			return nil, fmt.Errorf("live: the asset route answered a redirect with no location")
		}
		return fetchURL(ctx, location)
	}
	return payload, nil
}

// fetchURL reads a picture from where the API said it is. No session goes with it: the address is already signed.
func fetchURL(ctx context.Context, address string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("live: fetching a picture answered %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 32<<20))
}
