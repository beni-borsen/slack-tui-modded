package tui

import (
	"encoding/json"
	"hash/fnv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/cache"
)

// flushEvery is how often changed state is written to the cache.
const flushEvery = 3 * time.Second

type cacheMsg struct {
	snap cache.Snapshot
	err  error
}

type threadCacheMsg struct {
	conv, ts string
	list     cache.List
}

type flushTick struct{}

type bootRetry struct{}

// SetCache gives the model a local cache to start from and write to.
func (m *Model) SetCache(c *cache.Cache) { m.cache = c }

func (m *Model) loadCache() tea.Cmd {
	if m.cache == nil {
		return nil
	}
	return func() tea.Msg {
		s, err := m.cache.Load()
		return cacheMsg{s, err}
	}
}

// applyCache draws the last session's state until the API catches up. It
// is skipped when the API already answered.
func (m *Model) applyCache(s cache.Snapshot) {
	if m.booted || s.Empty() {
		return
	}
	for _, u := range s.Users {
		m.users[u.ID] = u
		if t := u.TeamID; t != "" && t != m.client.TeamID {
			m.extUsers[u.ID] = true
		}
	}
	for _, c := range s.Conversations {
		c.Cached = true
		m.mergeConv(c)
	}
	if len(m.emoji) == 0 && s.Emoji != nil {
		m.emoji = s.Emoji
	}
	if m.bots == nil {
		m.bots = map[string]string{}
	}
	for id, name := range s.Bots {
		m.bots[id] = name
	}
	for id, l := range s.Lists {
		if _, ok := m.convs[id]; ok {
			m.lists[id] = &msgList{msgs: l.Msgs, hasMore: l.HasMore, loaded: true, sel: -1}
		}
	}
	m.booted, m.fromCache = true, true
	m.rebuildSidebar()
}

func (m *Model) loadThreadCache(conv, ts string) tea.Cmd {
	if m.cache == nil {
		return nil
	}
	return func() tea.Msg {
		l, ok := m.cache.LoadList(cache.ThreadKey(conv, ts))
		if !ok {
			return nil
		}
		return threadCacheMsg{conv, ts, l}
	}
}

// applyThreadCache shows a cached thread while its replies load.
func (m *Model) applyThreadCache(msg threadCacheMsg) {
	t := m.thread
	if t == nil || t.conv != msg.conv || t.ts != msg.ts || t.list.loaded {
		return
	}
	t.cached = map[string]bool{}
	for _, x := range msg.list.Msgs {
		t.list.msgs = upsert(t.list.msgs, x)
		t.cached[x.TS] = true
	}
}

// touch marks the lists showing a conversation (and its open thread) as
// changed, so the next flush saves them.
func (m *Model) touch(convID string) {
	if m.cache == nil {
		return
	}
	if l, ok := m.lists[convID]; ok && l.loaded {
		m.dirty[convID] = l
	}
	if t := m.thread; t != nil && t.conv == convID {
		// The pointer stays valid after the thread is closed.
		m.dirty[cache.ThreadKey(t.conv, t.ts)] = &t.list
	}
}

func (m *Model) flushLater() tea.Cmd {
	if m.cache == nil {
		return nil
	}
	return tea.Tick(flushEvery, func(time.Time) tea.Msg { return flushTick{} })
}

// Flush writes what changed since the last flush. It is also called once
// when the program ends.
func (m *Model) Flush() {
	if m.cache == nil || !m.booted {
		return
	}
	for key, l := range m.dirty {
		if len(l.msgs) > 0 {
			m.cache.SaveList(key, l.msgs, l.hasMore)
		}
		delete(m.dirty, key)
	}
	users := make([]backend.User, 0, len(m.users))
	for _, u := range m.users {
		users = append(users, u)
	}
	convs := make([]backend.Conversation, 0, len(m.convs))
	for _, c := range m.convs {
		convs = append(convs, *c)
	}
	bots := map[string]string{}
	for id, name := range m.bots {
		if name != "" {
			bots[id] = name
		}
	}
	if m.changed("users", users) {
		m.cache.SaveUsers(users)
	}
	if m.changed("convs", convs) {
		m.cache.SaveConversations(convs)
	}
	if m.changed("bots", bots) {
		m.cache.SaveBots(bots)
	}
}

// changed reports whether v differs from what was last saved under key.
// Slices built from maps come in random order, so they are hashed as maps
// by ID: encoding/json writes map keys sorted.
func (m *Model) changed(key string, v any) bool {
	var b []byte
	switch v := v.(type) {
	case []backend.User:
		byID := make(map[string]backend.User, len(v))
		for _, u := range v {
			byID[u.ID] = u
		}
		b, _ = json.Marshal(byID)
	case []backend.Conversation:
		byID := make(map[string]backend.Conversation, len(v))
		for _, c := range v {
			byID[c.ID] = c
		}
		b, _ = json.Marshal(byID)
	default:
		b, _ = json.Marshal(v)
	}
	h := fnv.New64a()
	h.Write(b)
	sum := h.Sum64()
	if m.saved[key] == sum {
		return false
	}
	m.saved[key] = sum
	return true
}
