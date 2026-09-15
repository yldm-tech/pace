package httpsafe

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrTooManyRedirects is what following a chain past its limit produces, which on the Python side is requests.TooManyRedirects.
var ErrTooManyRedirects = errors.New("too many redirects")

// redirectStatuses are the five 3xx codes that carry a Location worth following.
var redirectStatuses = map[int]bool{301: true, 302: true, 303: true, 307: true, 308: true}

// Fetch is pinned_fetch for a method with no body. It resolves the host, validates every address, then connects to a validated IP literal so no second lookup can happen between the check and the connection, and it never follows a redirect on its own.
//
// Unlike the webhook's Post it tries each resolved address in turn, which is what the Python client does and what keeps a dual-stack host reachable when only one of its families answers.
func Fetch(ctx context.Context, method, target string, settings Settings, headers map[string]string, timeout time.Duration, limit int64) (*Response, error) {
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

	requireSafe := !HostAllowed(hostname, settings.AllowedHosts)
	addresses, err := ResolveAndValidate(hostname, settings.AllowedIPs, requireSafe)
	if err != nil {
		return nil, RejectedError{Reason: err.Error()}
	}

	var lastErr error
	for _, address := range addresses {
		pinned := address
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, net.JoinHostPort(pinned.String(), port))
			},
			TLSClientConfig: &tls.Config{ServerName: hostname},
			Proxy:           nil,
		}
		client := &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		request, err := http.NewRequestWithContext(ctx, method, target, nil)
		if err != nil {
			return nil, RejectedError{Reason: "Invalid URL"}
		}
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		request.Host = parsed.Host

		response, err := client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		payload, err := io.ReadAll(io.LimitReader(response.Body, limit))
		response.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return &Response{StatusCode: response.StatusCode, Headers: response.Header, Body: string(payload)}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no reachable address for host: %s", hostname)
}

// FetchFollowingRedirects follows a chain by hand, re-resolving, re-validating and re-pinning every hop, and answers with the final response and the url it came from.
//
// Letting the client follow them would re-resolve each hop and reopen the rebinding window, and a Location can point at an internal host that the first check never saw.
func FetchFollowingRedirects(ctx context.Context, method, target string, settings Settings, headers map[string]string, timeout time.Duration, maxRedirects int, limit int64) (*Response, string, error) {
	current := target
	for redirects := 0; ; redirects++ {
		response, err := Fetch(ctx, method, current, settings, headers, timeout, limit)
		if err != nil {
			return nil, current, err
		}
		if !redirectStatuses[response.StatusCode] {
			return response, current, nil
		}
		location := response.Headers.Get("Location")
		if location == "" {
			return response, current, nil
		}
		if redirects >= maxRedirects {
			return nil, current, ErrTooManyRedirects
		}
		base, err := url.Parse(current)
		if err != nil {
			return nil, current, RejectedError{Reason: "Invalid URL"}
		}
		next, err := url.Parse(strings.TrimSpace(location))
		if err != nil {
			return nil, current, RejectedError{Reason: "Invalid URL"}
		}
		current = base.ResolveReference(next).String()
	}
}
