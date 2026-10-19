package backend

import (
	"context"
	"fmt"
	"time"

	"github.com/slack-go/slack"
)

// SharedFile is a file in a conversation's file list.
type SharedFile struct {
	File
	User    string
	Created time.Time
	Size    int    // bytes
	Kind    string // Slack's name for the type: "PDF", "PNG", "Google Docs"
}

// FilesPage is one page of a conversation's files, newest first.
type FilesPage struct {
	Files              []SharedFile
	Total, Page, Pages int
}

// ConvFiles lists the files shared in a conversation (files.list).
func (c *Client) ConvFiles(ctx context.Context, id string, page int) (FilesPage, error) {
	var fs []slack.File
	var pg *slack.Paging
	err := retry(ctx, func() (err error) {
		fs, pg, err = c.api.GetFilesContext(ctx, slack.GetFilesParameters{Channel: id, Count: 50, Page: page})
		return err
	})
	if err != nil {
		return FilesPage{}, fmt.Errorf("files.list: %w", err)
	}
	out := FilesPage{Page: page, Pages: 1}
	if pg != nil {
		out.Total, out.Page, out.Pages = pg.Total, pg.Page, pg.Pages
	}
	for _, f := range fs {
		out.Files = append(out.Files, SharedFile{
			File: fromFile(f), User: f.User, Created: f.Created.Time(), Size: f.Size, Kind: f.PrettyType,
		})
	}
	return out, nil
}

// FileShare finds the message that shared a file in a conversation;
// files.list leaves that out. threadTS is set for a reply.
func (c *Client) FileShare(ctx context.Context, fileID, conv string) (ts, threadTS string, err error) {
	var f *slack.File
	err = retry(ctx, func() (err error) {
		f, _, _, err = c.api.GetFileInfoContext(ctx, fileID, 1, 1)
		return err
	})
	if err != nil {
		return "", "", fmt.Errorf("files.info: %w", err)
	}
	for _, shares := range []map[string][]slack.ShareFileInfo{f.Shares.Public, f.Shares.Private} {
		if s := shares[conv]; len(s) > 0 {
			return s[0].Ts, s[0].ThreadTs, nil
		}
	}
	return "", "", fmt.Errorf("files.info: no message shares it here")
}
