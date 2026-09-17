package httpsafe

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Settings carries the operator allowlists, which are
// WEBHOOK_ALLOWED_IPS and WEBHOOK_ALLOWED_HOSTS on the Django side.
type Settings struct {
	AllowedIPs   []netip.Prefix
	AllowedHosts []string
}

// ParseAllowedIPs reads the comma separated CIDR list the way settings.py does,
// skipping entries it cannot parse rather than failing to start.
func ParseAllowedIPs(raw string) ([]netip.Prefix, []string) {
	var prefixes []netip.Prefix
	var skipped []string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			skipped = append(skipped, entry)
			continue
		}
		// settings.py parses with strict=False, so host bits are tolerated.
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, skipped
}

// ParseAllowedHosts reads the comma separated host list settings.py builds.
func ParseAllowedHosts(raw string) []string {
	var hosts []string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(entry), "."))
		if entry != "" {
			hosts = append(hosts, entry)
		}
	}
	return hosts
}

// Response is what a caller needs from a pinned request.
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       string
}

// RejectedError marks a URL that failed validation. It maps onto the Python
// side's ValueError, which the webhook task treats as not retryable.
type RejectedError struct{ Reason string }

func (err RejectedError) Error() string { return err.Reason }

// maxResponseBody bounds what is read back from a webhook endpoint, since the
// body is stored in webhook_logs.
const maxResponseBody = 1 << 20

// Post is pinned_fetch for the one method Plane uses. It resolves the host,
// validates every address, then connects to the validated IP literal so no
// second DNS lookup can happen between the check and the connection. The
// original hostname is still used for the Host header, TLS SNI and certificate
// verification. Redirects are never followed: a Location could point at an
// internal host, and following it would reopen the rebinding window.
func Post(ctx context.Context, target string, settings Settings, headers map[string]string, body []byte, timeout time.Duration) (*Response, error) {
	return do(ctx, http.MethodPost, target, settings, headers, body, timeout)
}

// Get is Post's counterpart, for reading rather than sending. It is pinned the same way, and exists so that a caller who needs to read from an operator-supplied address cannot accidentally do it with a plain client -- one that would follow a redirect straight past every check above.
func Get(ctx context.Context, target string, settings Settings, headers map[string]string, timeout time.Duration) (*Response, error) {
	return do(ctx, http.MethodGet, target, settings, headers, nil, timeout)
}

// do is the pinned request both methods are. Keeping it in one place is what makes the guarantees above true of every request rather than of the one that happened to be written carefully.
func do(ctx context.Context, method, target string, settings Settings, headers map[string]string, body []byte, timeout time.Duration) (*Response, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, RejectedError{Reason: "Invalid URL"}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, RejectedError{Reason: "Invalid URL scheme. Only HTTP and HTTPS are allowed"}
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return nil, RejectedError{Reason: "Invalid URL: No hostname found"}
	}
	port := parsed.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[parsed.Scheme]
	}

	// A host on the operator's list skips the block check but still gets
	// pinned, because pinning defeats rebinding even for trusted hosts.
	requireSafe := !HostAllowed(hostname, settings.AllowedHosts)
	addresses, err := ResolveAndValidate(hostname, settings.AllowedIPs, requireSafe)
	if err != nil {
		return nil, RejectedError{Reason: err.Error()}
	}
	pinned := addresses[0]

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			// Ignore the address the resolver would produce and use the one
			// already validated.
			return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, net.JoinHostPort(pinned.String(), port))
		},
		TLSClientConfig: &tls.Config{ServerName: hostname},
		Proxy:           nil, // never route through an ambient proxy
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, RejectedError{Reason: "Invalid URL"}
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	request.Host = parsed.Host

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &Response{StatusCode: response.StatusCode, Headers: response.Header, Body: string(payload)}, nil
}
