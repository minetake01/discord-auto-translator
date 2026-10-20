package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errRemoteContentTooLarge marks responses whose body exceeded the fetch cap.
var errRemoteContentTooLarge = errors.New("remote content exceeds its size cap")

// remoteFetchOptions carries the per-purpose timeout and body ceiling of one
// outbound GET. Whether failure means failing the whole operation
// (avatars) or falling back (attachment and OGP page lookups) is the
// caller's decision, so the fetch itself only reports errors.
type remoteFetchOptions struct {
	timeout  time.Duration
	maxBytes int64 // 0 means unlimited
	accept   string
}

// remoteContent is a successful response body plus response metadata.
type remoteContent struct {
	body        []byte
	contentType string
	finalURL    string
}

// validRemoteHTTPURL reports whether rawURL is an absolute http(s) URL with a
// host. It is the single URL-shape check for outbound fetches; purpose-driven
// constraints (like the webhook avatar length cap) stay with their callers.
func validRemoteHTTPURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid HTTP URL %q", rawURL)
	}
	return nil
}

// fetchRemoteURL issues one outbound GET and reads the body under the shared
// URL-shape, status, and size-cap contract.
func fetchRemoteURL(ctx context.Context, client *http.Client, rawURL string, opts remoteFetchOptions) (remoteContent, error) {
	if err := validRemoteHTTPURL(rawURL); err != nil {
		return remoteContent{}, err
	}
	if client == nil {
		return remoteContent{}, errors.New("remote fetch: nil http client")
	}
	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return remoteContent{}, fmt.Errorf("create remote request for %q: %w", rawURL, err)
	}
	if opts.accept != "" {
		req.Header.Set("Accept", opts.accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return remoteContent{}, fmt.Errorf("fetch %q: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return remoteContent{}, fmt.Errorf("fetch %q: HTTP %d", rawURL, resp.StatusCode)
	}
	reader := io.Reader(resp.Body)
	if opts.maxBytes > 0 {
		reader = io.LimitReader(resp.Body, opts.maxBytes+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return remoteContent{}, fmt.Errorf("read %q: %w", rawURL, err)
	}
	if opts.maxBytes > 0 && int64(len(body)) > opts.maxBytes {
		return remoteContent{}, fmt.Errorf("%q: %w (%d bytes allowed)", rawURL, errRemoteContentTooLarge, opts.maxBytes)
	}
	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return remoteContent{body: body, contentType: resp.Header.Get("Content-Type"), finalURL: finalURL}, nil
}
