// Package tui is the Bubble Tea front end.
package tui

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/config"
	"github.com/magnusdalkvist/slack-tui/internal/kitty"
)

type focus int

const (
	fSidebar focus = iota
	fMessages
	fThread
)

// unreadsID is the sidebar entry for the Unreads view.
const unreadsID = "\x00unreads"

// Nerd Font glyphs for the Unreads entry and for people (DMs).
const (
	unreadsIcon = "\U000f0117"
	userIcon    = "\uf007"
	groupIcon   = "\uedca"
	lockIcon    = "\uf456" // private channels
)

const pageSize = 50

// msgList is one scrollable list of messages: a conversation or a thread.
type msgList struct {
	msgs    []backend.Message
	loaded  bool
	loading bool
	hasMore bool
	sel     int // selected message; -1 follows the newest
	top     int // first visible line, kept between renders
	// newSince is the read marker when the conversation was opened; the
	// "new" divider goes above the first message after it.
	newSince string
}

type threadState struct {
	conv string
	ts   string
	list msgList
}

type unreadsState struct {
	sel int // index into the unread conversations
	top int
}

type Model struct {
	ctx    context.Context
	client *backend.Client
	cfg    config.Config
	events chan any

	width, height int

	users      map[string]backend.User
	convs      map[string]*backend.Conversation
	lists      map[string]*msgList
	unreadMsgs map[string][]backend.Message // newest unread messages, for the Unreads view
	fetchingUR map[string]bool
	unreads    unreadsState

	side    []sideItem
	sideCur int
	sideTop int

	current string // conversation ID or unreadsID
	thread  *threadState
	focus   focus
	insert  bool
	input   textarea.Model
	viewer  *viewer       // full-window image viewer, when open
	attach  []*attachment // clipboard images going out with the next send

	switcher *switcher
	help     bool

	booted      bool
	bootErr     error
	conn        string
	infoPending int
	infoTotal   int
	status      string
	statusErr   bool
	statusSeq   int
	markSeq     map[string]int
	seenReplies map[string]bool // conv/ts of live replies already counted
	folded      map[string]bool // sidebar sections that are collapsed
	emoji       map[string]string

	gfx          *kitty.Writer // nil when the terminal can't draw images
	gfxReason    string
	images       map[string]*imgState // by file ID
	nextImgID    uint32
	imgIDBase    uint32
	cellW, cellH int
	imgSem       chan struct{}
	emojiImgs    map[string]*imgState // custom emoji images, by name

	picker  *picker
	editing *editState
	confirm *confirmState
	clip    io.Writer // where OSC 52 goes; nil means /dev/tty

	complete *completion // list while typing ":ta", "@an" or "#de"
	// composeTokens maps mentions picked from the list ("@Anna Jensen") to
	// their Slack form ("<@U123>"). Kept for the session: typing a picked
	// name again also becomes a mention, and a failed send keeps its mentions.
	composeTokens  map[string]string
	dismissed      string // query whose list was closed with esc
	allEmoji       []string
	allEmojiCustom int // len(m.emoji) when allEmoji was built
}

func New(ctx context.Context, client *backend.Client, cfg config.Config) *Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 40000
	ta.SetHeight(1)
	ta.FocusedStyle.CursorLine = ta.FocusedStyle.CursorLine.UnsetBackground()
	ta.FocusedStyle.Base = ta.FocusedStyle.Base.UnsetBorderStyle()
	ta.BlurredStyle = ta.FocusedStyle
	ta.KeyMap.InsertNewline.SetEnabled(false)

	return &Model{
		ctx:           ctx,
		client:        client,
		cfg:           cfg,
		events:        make(chan any, 256),
		users:         map[string]backend.User{},
		convs:         map[string]*backend.Conversation{},
		lists:         map[string]*msgList{},
		unreadMsgs:    map[string][]backend.Message{},
		fetchingUR:    map[string]bool{},
		markSeq:       map[string]int{},
		seenReplies:   map[string]bool{},
		folded:        map[string]bool{},
		emoji:         map[string]string{},
		images:        map[string]*imgState{},
		composeTokens: map[string]string{},
		emojiImgs:     map[string]*imgState{},
		imgSem:        make(chan struct{}, 3),
		current:       unreadsID,
		focus:         fSidebar,
		input:         ta,
		conn:          "connecting",
	}
}

// --- Bubble Tea messages ---------------------------------------------------

type bootMsg struct {
	users []backend.User
	convs []backend.Conversation
	err   error
}

type convsMsg struct {
	convs []backend.Conversation
	err   error
}

type infoMsg struct {
	conv backend.Conversation
	err  error
}

type historyMsg struct {
	id      string
	msgs    []backend.Message
	hasMore bool
	older   bool
	err     error
}

type repliesMsg struct {
	id, ts string
	msgs   []backend.Message
	err    error
}

type sinceMsg struct {
	id   string
	msgs []backend.Message
	err  error
}

type postedMsg struct {
	id, threadTS, ts, text string
	err                    error
}

type errMsg struct{ err error }

type markTick struct {
	id  string
	seq int
}

type refreshTick struct{}

type statusClear struct{ seq int }

// --- commands ---------------------------------------------------------------

func (m *Model) Init() tea.Cmd {
	go m.client.Listen(m.ctx, m.events)
	for _, name := range config.LoadState().Folded {
		m.folded[name] = true
	}
	return tea.Batch(m.boot(), m.loadEmoji(), m.waitEvent(), textarea.Blink)
}

func (m *Model) waitEvent() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-m.events:
			return ev
		case <-m.ctx.Done():
			return nil
		}
	}
}

func (m *Model) boot() tea.Cmd {
	return func() tea.Msg {
		var (
			wg    sync.WaitGroup
			users []backend.User
			convs []backend.Conversation
			uerr  error
			cerr  error
		)
		wg.Add(2)
		go func() { defer wg.Done(); users, uerr = m.client.Users(m.ctx) }()
		go func() { defer wg.Done(); convs, cerr = m.client.Conversations(m.ctx) }()
		wg.Wait()
		if cerr != nil {
			return bootMsg{err: cerr}
		}
		return bootMsg{users: users, convs: convs, err: uerr}
	}
}

type emojiMsg struct {
	emoji map[string]string
	err   error
}

func (m *Model) loadEmoji() tea.Cmd {
	return func() tea.Msg {
		e, err := m.client.CustomEmoji(m.ctx)
		return emojiMsg{e, err}
	}
}

func (m *Model) reloadConvs() tea.Cmd {
	return func() tea.Msg {
		convs, err := m.client.Conversations(m.ctx)
		return convsMsg{convs: convs, err: err}
	}
}

// loadInfos fetches read markers in the background, a few at a time, and
// delivers the results through the event channel as they arrive.
func (m *Model) loadInfos(ids []string) {
	if len(ids) == 0 {
		return
	}
	m.infoPending += len(ids)
	m.infoTotal += len(ids)
	jobs := make(chan string)
	for range 4 {
		go func() {
			for id := range jobs {
				c, err := m.client.Info(m.ctx, id)
				if err != nil {
					c.ID = id
				}
				send(m.ctx, m.events, infoMsg{conv: c, err: err})
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, id := range ids {
			select {
			case jobs <- id:
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

func send(ctx context.Context, ch chan<- any, v any) {
	select {
	case ch <- v:
	case <-ctx.Done():
	}
}

func (m *Model) loadHistory(id string, older bool) tea.Cmd {
	l := m.list(id)
	if l.loading {
		return nil
	}
	l.loading = true
	before := ""
	if older && len(l.msgs) > 0 {
		before = l.msgs[0].TS
	}
	return func() tea.Msg {
		msgs, more, err := m.client.History(m.ctx, id, before, pageSize)
		return historyMsg{id: id, msgs: msgs, hasMore: more, older: older, err: err}
	}
}

func (m *Model) loadReplies(id, ts string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := m.client.Replies(m.ctx, id, ts)
		return repliesMsg{id: id, ts: ts, msgs: msgs, err: err}
	}
}

func (m *Model) loadSince(id string) tea.Cmd {
	c := m.convs[id]
	if c == nil || m.fetchingUR[id] {
		return nil
	}
	m.fetchingUR[id] = true
	after := c.LastRead
	return func() tea.Msg {
		msgs, err := m.client.Since(m.ctx, id, after, 20)
		return sinceMsg{id: id, msgs: msgs, err: err}
	}
}

func (m *Model) post(id, threadTS, text string) tea.Cmd {
	return func() tea.Msg {
		ts, err := m.client.Post(m.ctx, id, text, threadTS)
		return postedMsg{id: id, threadTS: threadTS, ts: ts, text: text, err: err}
	}
}

func (m *Model) mark(id, ts string) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.Mark(m.ctx, id, ts); err != nil {
			return errMsg{fmt.Errorf("mark read: %w", err)}
		}
		return nil
	}
}

func (m *Model) refreshLater() tea.Cmd {
	return tea.Tick(time.Duration(m.cfg.RefreshMinutes)*time.Minute, func(time.Time) tea.Msg { return refreshTick{} })
}

func (m *Model) setStatus(s string, isErr bool) tea.Cmd {
	m.status, m.statusErr = s, isErr
	m.statusSeq++
	seq := m.statusSeq
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return statusClear{seq} })
}

// --- state helpers ----------------------------------------------------------

func (m *Model) list(id string) *msgList {
	l, ok := m.lists[id]
	if !ok {
		l = &msgList{sel: -1}
		m.lists[id] = l
	}
	return l
}

// upsert inserts a message in timestamp order, or replaces it if present.
func upsert(msgs []backend.Message, msg backend.Message) []backend.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].TS == msg.TS {
			msgs[i] = msg
			return msgs
		}
		if backend.TSLess(msgs[i].TS, msg.TS) {
			return slices.Insert(msgs, i+1, msg)
		}
	}
	return slices.Insert(msgs, 0, msg)
}

func remove(msgs []backend.Message, ts string) []backend.Message {
	return slices.DeleteFunc(msgs, func(x backend.Message) bool { return x.TS == ts })
}

func (m *Model) mergeConv(c backend.Conversation) {
	old, ok := m.convs[c.ID]
	if !ok {
		cc := c
		m.convs[c.ID] = &cc
		return
	}
	// Keep the name/kind from the list, and never move markers backwards:
	// live events may know about newer messages than the API call did.
	if c.InfoLoaded {
		if backend.TSLess(old.LastRead, c.LastRead) || !old.InfoLoaded {
			old.LastRead = c.LastRead
		}
		if backend.TSLess(old.Latest, c.Latest) {
			old.Latest = c.Latest
		}
		if old.IsDM() {
			old.Mentions = c.Mentions
		}
		if !old.Unread() {
			old.Mentions = 0
		}
		old.InfoLoaded = true
	}
	if c.Topic != "" {
		old.Topic = c.Topic
	}
}

// markRead moves the read marker to the newest message and tells Slack.
func (m *Model) markRead(id string) tea.Cmd {
	c := m.convs[id]
	if c == nil {
		return nil
	}
	c.Mentions = 0
	delete(m.unreadMsgs, id)
	if !c.Unread() {
		return nil
	}
	c.LastRead = c.Latest
	return m.mark(id, c.Latest)
}

func (m *Model) open(id string) tea.Cmd {
	if id != m.current && m.thread != nil {
		m.thread = nil
	}
	m.current = id
	m.rebuildSidebar() // a folded section shows its open conversation
	m.sideFind(id)
	if id == unreadsID {
		m.focus = fMessages
		return m.fetchUnreads()
	}
	c := m.convs[id]
	if c == nil {
		return nil
	}
	l := m.list(id)
	l.sel = -1
	l.newSince = ""
	if c.Unread() {
		l.newSince = c.LastRead
	}
	m.focus = fMessages
	// Always fetch the newest page: on first open it's the load, after that
	// it refreshes what live events may have missed.
	cmds := []tea.Cmd{m.loadHistory(id, false)}
	cmds = append(cmds, m.markRead(id))
	return tea.Batch(cmds...)
}

// sideFind moves the sidebar cursor to id and returns id.
func (m *Model) sideFind(id string) string {
	for i, it := range m.side {
		if it.id == id {
			m.sideCur = i
		}
	}
	return id
}

// unreadConvs lists unread conversations, newest first.
func (m *Model) unreadConvs() []*backend.Conversation {
	var out []*backend.Conversation
	for _, c := range m.convs {
		if c.Unread() {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b *backend.Conversation) int {
		if a.Latest == b.Latest {
			return strings.Compare(a.ID, b.ID)
		}
		if backend.TSLess(a.Latest, b.Latest) {
			return 1
		}
		return -1
	})
	return out
}

func (m *Model) fetchUnreads() tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range m.unreadConvs() {
		if _, ok := m.unreadMsgs[c.ID]; !ok {
			cmds = append(cmds, m.loadSince(c.ID))
		}
	}
	return tea.Batch(cmds...)
}

// infoOrder puts the conversations the user cares about first: custom
// sections, then DMs, then channels.
func (m *Model) infoOrder() []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, sec := range m.cfg.Sections {
		for _, ref := range sec.Items {
			add(m.resolveRef(ref))
		}
	}
	var dms, chans []string
	for id, c := range m.convs {
		if c.IsDM() {
			dms = append(dms, id)
		} else {
			chans = append(chans, id)
		}
	}
	slices.Sort(dms)
	slices.Sort(chans)
	for _, id := range dms {
		add(id)
	}
	for _, id := range chans {
		add(id)
	}
	return ids
}

// --- name resolution (render.Resolver) --------------------------------------

func (m *Model) UserName(id string) string {
	if u, ok := m.users[id]; ok {
		return u.Label()
	}
	return ""
}

func (m *Model) ChannelName(id string) string {
	if c, ok := m.convs[id]; ok && !c.IsDM() {
		return c.Name
	}
	return ""
}

func (m *Model) SelfID() string { return m.client.SelfID }

func (m *Model) CustomEmoji(name string) (string, bool) {
	v, ok := m.emoji[name]
	return v, ok
}

func (m *Model) senderName(msg backend.Message) string {
	if msg.User != "" {
		if n := m.UserName(msg.User); n != "" {
			return n
		}
	}
	if msg.BotName != "" {
		return msg.BotName
	}
	if msg.User != "" {
		return msg.User
	}
	return "unknown"
}

// convLabel is the sidebar/switcher name for a conversation.
func (m *Model) convLabel(c *backend.Conversation) string {
	switch c.Kind {
	case backend.KindDM:
		if c.UserID == m.client.SelfID {
			return m.UserName(c.UserID) + " (you)"
		}
		if n := m.UserName(c.UserID); n != "" {
			return n
		}
		return c.UserID
	case backend.KindGroup:
		// mpdm-anna--bob--carl-1 → anna, bob, carl
		raw := strings.TrimSuffix(strings.TrimPrefix(c.Name, "mpdm-"), "-1")
		var names []string
		for _, uname := range strings.Split(raw, "--") {
			if uname == m.users[m.client.SelfID].Name {
				continue
			}
			names = append(names, m.labelForUsername(uname))
		}
		return strings.Join(names, ", ")
	default:
		return c.Name
	}
}

func (m *Model) labelForUsername(name string) string {
	for _, u := range m.users {
		if u.Name == name {
			return u.Label()
		}
	}
	return name
}

// resolveRef turns a config entry ("#general", "@anna") into a conversation ID.
func (m *Model) resolveRef(ref string) string {
	ref = strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(ref, "#"):
		name := strings.ToLower(ref[1:])
		for id, c := range m.convs {
			if !c.IsDM() && strings.ToLower(c.Name) == name {
				return id
			}
		}
	case strings.HasPrefix(ref, "@"):
		name := strings.ToLower(ref[1:])
		for id, c := range m.convs {
			if c.Kind != backend.KindDM {
				continue
			}
			u := m.users[c.UserID]
			for _, n := range []string{u.Name, u.DisplayName, u.RealName} {
				if n != "" && strings.ToLower(n) == name {
					return id
				}
			}
		}
	}
	return ""
}
