package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

var httpClient = &http.Client{
	Timeout: 2 * time.Minute,
	// Never carry the token across a redirect to somewhere else.
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !slackHost(req.URL) {
			return fmt.Errorf("refusing redirect to %s", req.URL.Host)
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// slackHost is true only for https URLs on Slack's own domain: the user
// token goes in the request, so it must never reach any other host.
func slackHost(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (h == "slack.com" || strings.HasSuffix(h, ".slack.com"))
}

// DownloadEmoji fetches a custom emoji image. These are public files on
// Slack's CDN (*.slack-edge.com), so no token is sent, and only that domain
// is allowed.
func DownloadEmoji(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".slack-edge.com") {
		return nil, fmt.Errorf("not a Slack emoji URL: %s", u.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("emoji: %s", resp.Status)
	}
	const limit = 5 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && len(b) > limit {
		err = errors.New("emoji: over 5 MB")
	}
	return b, err
}

// Download fetches a private Slack file (url_private, thumbnails) with the
// user token, refusing anything larger than maxBytes.
func (c *Client) Download(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if !slackHost(u) {
		return nil, fmt.Errorf("not a Slack file URL: %s", u.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: %s", resp.Status)
	}
	// Slack answers an auth problem with a 200 HTML login page.
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		return nil, errors.New("download: got a login page — does the app have files:read?")
	}
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("download: file is %d MB, limit %d MB", resp.ContentLength>>20, maxBytes>>20)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("download: file is over the %d MB limit", maxBytes>>20)
	}
	return b, nil
}

// Upload is one file going out with a message.
type Upload struct {
	Name string
	Data []byte
}

// Upload shares files into a conversation (a thread when threadTS is set) as
// one message, with comment as its text.
func (c *Client) Upload(ctx context.Context, id, threadTS string, files []Upload, comment string) error {
	var done []slack.FileSummary
	for _, f := range files {
		var u *slack.GetUploadURLExternalResponse
		err := retry(ctx, func() (err error) {
			u, err = c.api.GetUploadURLExternalContext(ctx, slack.GetUploadURLExternalParameters{FileName: f.Name, FileSize: len(f.Data)})
			return err
		})
		if err != nil {
			return fmt.Errorf("upload %s: %w", f.Name, err)
		}
		err = retry(ctx, func() error {
			return c.api.UploadToURL(ctx, slack.UploadToURLParameters{UploadURL: u.UploadURL, Reader: bytes.NewReader(f.Data), Filename: f.Name})
		})
		if err != nil {
			return fmt.Errorf("upload %s: %w", f.Name, err)
		}
		done = append(done, slack.FileSummary{ID: u.FileID, Title: f.Name})
	}
	err := retry(ctx, func() error {
		_, err := c.api.CompleteUploadExternalContext(ctx, slack.CompleteUploadExternalParameters{
			Files: done, Channel: id, ThreadTimestamp: threadTS, InitialComment: comment,
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	return nil
}
