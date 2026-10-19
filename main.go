// slack-tui is a terminal Slack client that uses an official Slack app's
// user token (Web API) and app-level token (Socket Mode).
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slack-go/slack"
	_ "golang.org/x/image/webp"
	"golang.org/x/term"

	"github.com/magnusdalkvist/slack-tui/internal/auth"
	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/cache"
	"github.com/magnusdalkvist/slack-tui/internal/config"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
	"github.com/magnusdalkvist/slack-tui/internal/tui"
)

const usage = `slack — Slack in the terminal (slack-tui)

usage:
  slack                          start the client
  slack @person message…         send a direct message
  slack '#channel' message…      send to a channel (quote it: # starts a
  slack channel message…           comment in the shell, or leave it off)
  echo text | slack @person      send what comes in on stdin
  slack -n @person message…      dry run: show who it would go to, send nothing
  slack completion bash          print the bash tab-completion script
  slack login                    store your Slack app tokens in the keyring
  slack logout                   remove the stored tokens and cached messages
  slack doctor                   check tokens, scopes and API access (read-only)

People match on display name, full name or username; a partial name works
when only one person matches.

config: %s
`

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "":
		err = run()
	case "login":
		err = login()
	case "logout":
		err = auth.Delete()
		if err == nil {
			err = cache.RemoveAll()
		}
		if err == nil {
			fmt.Println("tokens and cached messages removed")
		}
	case "doctor":
		err = doctor()
	case "-h", "--help", "help":
		fmt.Printf(usage, config.Path())
	case "__complete": // called by the bash completion script
		if len(os.Args) > 2 {
			completeWord(os.Args[2])
		}
	case "__refresh-names":
		err = refreshNames()
	case "completion":
		if len(os.Args) < 3 || os.Args[2] != "bash" {
			err = errors.New("usage: slack completion bash")
			break
		}
		fmt.Print(bashCompletion)
	case "-n", "--dry-run":
		if len(os.Args) < 3 {
			err = errors.New("usage: slack -n @person message…")
			break
		}
		err = quickSend(os.Args[2], os.Args[3:], true)
	default:
		err = quickSend(cmd, os.Args[2:], false)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "slack:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config %s: %w", config.Path(), err)
	}
	tokens, err := auth.Load()
	if err != nil {
		return err
	}
	unlock, err := lockSingleInstance()
	if err != nil {
		return err
	}
	defer unlock()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()

	client := backend.New(tokens)
	actx, acancel := context.WithTimeout(ctx, 15*time.Second)
	err = client.Auth(actx)
	acancel()
	if err != nil {
		return err
	}

	m := tui.New(ctx, client, cfg)
	// The cache only makes the start faster; without it everything still works.
	if c, err := cache.Open(cache.Path(client.TeamID)); err == nil {
		m.SetCache(c)
		defer func() {
			m.Flush()
			c.Close()
		}()
	} else {
		fmt.Fprintln(os.Stderr, "slack: cache off:", err)
	}
	// Everything written to the terminal goes through one lock: the TUI's
	// frames and the image data.
	out := kitty.NewSyncFile(os.Stdout)
	if cfg.Images != "off" {
		if ok, reason := kitty.Support(); !ok {
			m.SetGraphics(nil, reason)
		} else {
			w := kitty.NewWriter(out, kitty.InTmux())
			m.SetGraphics(w, "")
			defer w.Close()
		}
	}
	m.SetClipboard(out)
	// Focus reports tell us when our tmux pane is shown again, to resend images.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithReportFocus(), tea.WithOutput(out))
	// Autowrap off while we run: when the terminal (or tmux) thinks a
	// character is wider than we measured, a full-width row would wrap and
	// scroll the whole screen, leaving stale rows behind. Without autowrap it
	// just loses its last cell.
	out.WriteString("\x1b[?7l")
	defer out.WriteString("\x1b[?7h")
	_, err = p.Run()
	return err
}

// lockSingleInstance refuses to start a second copy: Socket Mode spreads
// events across open connections, so two copies would each miss messages.
func lockSingleInstance() (func(), error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.OpenFile(filepath.Join(dir, "slack-tui.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("slack-tui is already running — Socket Mode would split live events between the two copies")
	}
	return func() { f.Close() }, nil
}

func login() error {
	fmt.Println("Paste the tokens from your Slack app (api.slack.com/apps). Input is hidden.")
	fmt.Println()
	user, err := prompt("User OAuth Token (OAuth & Permissions, starts with xoxp-): ")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(user, "xoxp-") {
		return errors.New("that is not a user token (expected xoxp-…)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	api := slack.New(user)
	who, err := api.AuthTestContext(ctx)
	if err != nil {
		return fmt.Errorf("user token rejected: %w", err)
	}
	fmt.Printf("  ✓ %s in %s\n\n", who.User, who.Team)

	app, err := prompt("App-Level Token (Basic Information → App-Level Tokens, starts with xapp-): ")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(app, "xapp-") {
		return errors.New("that is not an app-level token (expected xapp-…)")
	}
	// Asks Slack for a Socket Mode URL without connecting.
	if _, _, err := slack.New(user, slack.OptionAppLevelToken(app)).StartSocketModeContext(ctx); err != nil {
		return fmt.Errorf("app-level token rejected (does it have connections:write, and is Socket Mode on?): %w", err)
	}
	fmt.Println("  ✓ Socket Mode available")

	if err := auth.Save(auth.Tokens{User: user, App: app}); err != nil {
		return err
	}
	fmt.Println("\nSaved to the system keyring. Run `slack-tui` to start.")
	return nil
}

func prompt(label string) (string, error) {
	fmt.Print(label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return strings.TrimSpace(string(b)), err
	}
	s, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(s), err
}

// doctor makes read-only calls to check the setup and what the API returns.
func doctor() error {
	tokens, err := auth.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := backend.New(tokens)
	if err := c.Auth(ctx); err != nil {
		return err
	}
	fmt.Printf("✓ signed in as %s in %s (%s)\n", c.SelfID, c.TeamName, c.TeamURL)

	users, err := c.Users(ctx)
	if err != nil {
		fmt.Println("✕ users.list:", err)
	} else {
		fmt.Printf("✓ users.list: %d users\n", len(users))
	}

	convs, err := c.Conversations(ctx)
	if err != nil {
		return err
	}
	counts := map[backend.Kind]int{}
	for _, cv := range convs {
		counts[cv.Kind]++
	}
	fmt.Printf("✓ users.conversations: %d channels, %d private, %d DMs, %d group DMs\n",
		counts[backend.KindChannel], counts[backend.KindPrivate], counts[backend.KindDM], counts[backend.KindGroup])

	// Check what conversations.info gives us for read markers.
	checked := map[backend.Kind]bool{}
	for _, cv := range convs {
		if checked[cv.Kind] {
			continue
		}
		checked[cv.Kind] = true
		start := time.Now()
		info, err := c.Info(ctx, cv.ID)
		if err != nil {
			fmt.Printf("✕ conversations.info %s: %v\n", cv.ID, err)
			continue
		}
		fmt.Printf("✓ conversations.info kind=%d last_read=%q latest=%q unread=%v mentions=%d (%s)\n",
			cv.Kind, info.LastRead, info.Latest, info.Unread(), info.Mentions, time.Since(start).Round(time.Millisecond))
	}

	// Image thumbnails: files:read, the Slack-only download path, decoding.
	// Only the format and size are reported; nothing is saved.
	switch f, err := c.LatestImage(ctx); {
	case err != nil:
		fmt.Println("✕", err)
	case f == nil:
		fmt.Println("- no image files to test thumbnail download with")
	default:
		data, err := c.Download(ctx, f.Thumb, 25<<20)
		if err != nil {
			fmt.Println("✕ thumbnail download:", err)
			break
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			fmt.Println("✕ thumbnail decode:", err)
			break
		}
		fmt.Printf("✓ thumbnail download: %s %dx%d, %d KB (original %s, %dx%d)\n", format, cfg.Width, cfg.Height, len(data)>>10, f.Mimetype, f.W, f.H)
	}
	if ok, why := kitty.Support(); ok {
		fmt.Println("✓ inline images: terminal supports kitty graphics")
	} else {
		fmt.Println("- inline images off:", why)
	}

	if _, _, err := slack.New(tokens.User, slack.OptionAppLevelToken(tokens.App)).StartSocketModeContext(ctx); err != nil {
		fmt.Println("✕ Socket Mode:", err)
	} else {
		fmt.Println("✓ Socket Mode available")
	}
	return nil
}
