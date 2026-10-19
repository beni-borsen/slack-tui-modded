# slack-tui

Slack in the terminal, using an official Slack app: a user token (`xoxp-`) for the Web API and an
app-level token (`xapp-`) for live events over Socket Mode. Go + Bubble Tea.

## Setup

1. Create the app at api.slack.com/apps → *From a manifest* (see `manifest.yaml`), install it to the workspace.
2. Create an app-level token with `connections:write` (Basic Information → App-Level Tokens).
3. `go build -o slack-tui . && ./slack-tui login` — paste both tokens; they go into the system keyring.
4. `./slack-tui doctor` checks access (read-only), `./slack-tui` starts the client.

Only one copy can run at a time: Socket Mode splits events between open connections, so a second
copy would make both miss messages.

## Command line

Link the binary as `slack` to use it from any shell: `ln -s "$PWD/slack-tui" ~/.local/bin/slack`
(this shadows the desktop app's `/usr/bin/slack` in shells only; its launcher entry uses the full path).

```
slack                              # the client
slack @anna 'on my way'            # direct message; a unique partial name works
slack deploys 'deploy ok'          # channel ('#deploys' works too, quoted)
git log -1 --oneline | slack @anna # message from stdin
slack -n @an hi                    # dry run: who would get it (lists all matches if ambiguous)
```

Tab completion (bash): `slack completion bash > ~/.local/share/bash-completion/completions/slack`.
It completes `@people` (to their shown name when that's one unique word, else their username) and
channel names, from `~/.cache/slack-tui/names.json`. The client refreshes that file on start, and
completion refreshes it in the background when it's over a day old.

## Config

`~/.config/slack-tui/config.toml` (optional):

```toml
time_format = "15:04"     # Go time layout
sidebar_width = 30
refresh_minutes = 5       # re-check unread markers (catches messages read on another device)
presence_minutes = 3      # re-check who is active among the DMs in the sidebar; -1 turns it off
images = "auto"           # inline images when the terminal can draw them, or "off"
image_max_cols = 40       # thumbnail size limit, in cells
image_max_rows = 12
image_viewer = "loupe"    # opens images with `o` (default: loupe if installed); other files use xdg-open

# Custom sidebar sections, above the built-in Channels, Direct messages (one-to-one first,
# then groups, each by recent activity) and External (Slack Connect channels and people).
# Slack's own sections are not in the public API.
[[section]]
name = "Team"
icon = ":partyparrot:"   # optional emoji before the name; custom workspace emoji work too
items = ["#dev", "#deploys", "@anna"]   # "#channel" or "@username / display name / real name"

[[section]]
name = "News"
items = ["#news"]
```

## Keys

| key | |
|---|---|
| `ctrl+k` / `space` | jump to conversation |
| `j` `k`, `g` `G`, `ctrl+d` `ctrl+u` | move |
| `h` `l`, `tab` | move between sidebar / messages / thread |
| `enter` | open conversation, or open the selected message's thread |
| `i` / `a` | write in the focused pane (channel or thread) |
| `enter` / `alt+enter`, `ctrl+j` | send / new line (insert mode) |
| `ctrl+b` / `alt+i` (insert mode) | bold / italic the word at the cursor (again to undo; with no word, starts a `**` pair). `[label](https://…)` is sent as a link |
| paste an image, `ctrl+v` | attach the clipboard image (repeat for up to 10 in one message); `enter` uploads it with the typed text, `ctrl+x` drops it (insert mode) |
| `:ta` `@an` `#de` (insert mode) | complete emoji, people (also @here/@channel/@everyone) and channels: `tab`/`enter` inserts, `↑` `↓` choose, `esc` closes. People and channels are sent as real mentions |
| `esc` | back to normal mode |
| `/` | search messages: Slack's own query syntax (`from:@anna in:#dev after:2026-01-01 "exact words"`); typing `@`, `#` or a name after `from:`/`in:` suggests people and channels (Slack's search matches handles, not display names, so pick from the list); `enter` goes to the message (its thread for replies), `o` opens it in Slack |
| `:` | command line (below): `tab` completes, `↑` `↓` history, `esc` leaves |
| `u` | next unread conversation |
| `r` | mark conversation read |
| `c` | copy the selected message's text (OSC 52, so it works through tmux and ssh) |
| `+` | react: emoji picker (enter adds, or removes a reaction you already have) |
| `e` / `↑` in an empty input | edit your message / your last message (enter saves, esc cancels) |
| `D` | delete your message (asks y/n) |
| `o` | open the selected message's file (images in `image_viewer`) or link (browser); with several, a chooser: `1`-`9` or `enter` opens |
| `v` | full-window image viewer from the selected message: `←` `→` step through every image in the conversation or thread, `o` opens externally, `esc` closes |
| `x` | close thread |
| `?` | help |

## Commands

| command | |
|---|---|
| `:status :emoji: text [expiry]` | set your status; expiry is `30m`, `2h`, `1h30m`, `1d`, `until 17:00` or `until tomorrow` (midnight). Emoji complete as you type |
| `:status` / `:status clear` | show / clear your status |
| `:dnd 30m` / `:dnd until 17:00` / `:dnd off` | pause notifications (Do Not Disturb), or turn them back on; `:dnd` shows the state |
| `:away` / `:back` | show as away / let Slack decide again |

A unique prefix is enough (`:st`, `:d 1h`). Your DND, away state and status sit in the status bar.
People in the DM list get a green icon while active (checked every `presence_minutes`) and their
status emoji after the name; the open DM's header shows their status and when it ends.

## Images

Inline images use the kitty graphics protocol with Unicode placeholders, so they need ghostty or kitty.
Through tmux they also need `set -g allow-passthrough on`, and `set -g focus-events on` so images that
finished loading while the pane was hidden get sent again when it is shown. `slack-tui doctor` reports
whether images are on and tests a thumbnail download. The user token is only ever sent to `https://*.slack.com`.
Opened files are cached in `~/.cache/slack-tui/files/`.

Custom emoji are drawn as images too, two cells wide, fetched from Slack's public emoji CDN
(`*.slack-edge.com`, no token sent) and cached in `~/.cache/slack-tui/emoji/`.

## Cache

What the client last saw (people, conversations and read markers, custom emoji, the newest 200
messages of each conversation and the threads you opened) is kept in
`~/.cache/slack-tui/slack-<team>.db`, so the next start draws at once and catches up in the
background. Without a connection it keeps showing the cache and retries every 30 seconds.
`slack logout` deletes it along with the tokens.

## Limits of the official API

- No unread-count endpoint: on start the client calls `conversations.info` once per conversation
  (rate-limited, ~50/min), so unread markers fill in progressively.
- Reading a channel on another device sends no event; markers are re-checked every `refresh_minutes`.
- No typing indicators, huddles, drafts, or Slack's own sidebar sections.
- No presence events for user tokens: presence is polled with `users.getPresence`, one call per person.

## License

MIT
