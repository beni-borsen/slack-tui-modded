package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/names"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	// Whatever changed, start downloads for images and custom emoji that
	// came into view.
	return m, tea.Batch(cmd, m.scanImages(), m.scanEmoji(), m.scanBots(), m.scanUsers())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.cellW, m.cellH = cellSize()
		if m.viewer != nil {
			return m, m.loadViewer()
		}
		return m, nil

	case tea.FocusMsg:
		if m.viewer != nil {
			m.viewer.shown = "" // tmux may have dropped it while we were hidden
			m.showViewer()
		}
		return m, m.resendImages()

	case viewLoadedMsg:
		m.viewerLoaded(msg)
		return m, nil

	case imageMsg:
		st := m.images[msg.fileID]
		if st == nil {
			return m, nil
		}
		if msg.err != nil {
			st.status, st.err = imgFailed, msg.err.Error()
			return m, nil
		}
		st.status, st.cols, st.rows, st.png = imgReady, msg.cols, msg.rows, msg.png
		return m, nil

	case emojiImgMsg:
		if st := m.emojiImgs[msg.name]; st != nil {
			if msg.err != nil {
				st.status, st.err = imgFailed, msg.err.Error()
			} else {
				st.status, st.png, st.cols, st.rows = imgReady, msg.png, 2, 1
			}
		}
		return m, nil

	case reactedMsg:
		if msg.err != nil {
			undo := msg.ev
			undo.Added = !undo.Added
			m.applyReaction(undo)
			return m, m.setStatus("reaction: "+msg.err.Error(), true)
		}
		return m, nil

	case scheduledMsg:
		return m, m.scheduledResult(msg)

	case scheduledListMsg:
		return m, m.scheduledListResult(msg)

	case unscheduledMsg:
		return m, m.unscheduledResult(msg)

	case publicChansMsg:
		return m, m.publicResult(msg)

	case joinedMsg:
		return m, m.joinedResult(msg)

	case leftMsg:
		return m, m.leftResult(msg)

	case createdMsg:
		return m, m.createdResult(msg)

	case invitedMsg:
		return m, m.invitedResult(msg)

	case convFilesMsg:
		return m, m.sharedFilesResult(msg)

	case convLinksMsg:
		m.sharedLinksResult(msg)
		return m, nil

	case fileShareMsg:
		return m, m.fileShareResult(msg)

	case bookmarkedMsg:
		return m, m.bookmarkedResult(msg)

	case pinsMsg:
		m.pinsResult(msg)
		return m, nil

	case pinnedMsg:
		if msg.err != nil {
			m.setPinned(msg.conv, msg.ts, !msg.added)
			var cmd tea.Cmd
			if p := m.pins; p != nil && p.conv == msg.conv {
				cmd = m.loadPins(msg.conv) // put the unpinned row back
			}
			return m, tea.Batch(cmd, m.setStatus(msg.err.Error(), true))
		}
		if msg.added {
			return m, m.setStatus("pinned", false)
		}
		return m, m.setStatus("unpinned", false)

	case editedMsg:
		if msg.err != nil {
			m.editMessage(msg.conv, msg.old)
			return m, m.setStatus("edit failed: "+msg.err.Error(), true)
		}
		return m, nil

	case deletedMsg:
		if msg.err != nil {
			return m, m.setStatus("delete failed: "+msg.err.Error(), true)
		}
		m.removeMessage(msg.conv, msg.ts)
		return m, m.setStatus("message deleted", false)

	case savedMsg:
		return m, m.savedResult(msg)

	case openedMsg:
		if msg.err != nil {
			return m, m.setStatus("open "+msg.name+": "+msg.err.Error(), true)
		}
		return m, m.setStatus("opened "+msg.name, false)

	case tea.KeyMsg:
		return m, m.handleKey(msg)

	case searchMsg:
		return m, m.searchResult(msg)

	case cacheMsg:
		if msg.err != nil {
			return m, m.setStatus("cache: "+msg.err.Error(), true)
		}
		m.applyCache(msg.snap)
		return m, nil

	case threadCacheMsg:
		m.applyThreadCache(msg)
		return m, nil

	case flushTick:
		m.Flush()
		return m, m.flushLater()

	case bootRetry:
		return m, m.boot()

	case bootMsg:
		if msg.users == nil && msg.err != nil {
			if m.fromCache {
				// Offline, or Slack is down: keep showing the cache and try again.
				retry := tea.Tick(30*time.Second, func(time.Time) tea.Msg { return bootRetry{} })
				return m, tea.Batch(retry, m.setStatus("offline, showing cached messages: "+msg.err.Error(), true))
			}
			m.bootErr = msg.err
			return m, nil
		}
		for _, u := range msg.users {
			m.users[u.ID] = u
		}
		if m.fromCache {
			// Forget cached conversations the user has since left.
			live := map[string]bool{}
			for _, c := range msg.convs {
				live[c.ID] = true
			}
			for id := range m.convs {
				if !live[id] {
					delete(m.convs, id)
					delete(m.lists, id)
				}
			}
			m.fromCache = false
		}
		for _, c := range msg.convs {
			m.mergeConv(c)
		}
		m.booted = true
		m.rebuildSidebar()
		m.loadInfos(m.infoOrder())
		var cmd tea.Cmd
		if msg.err != nil {
			cmd = m.setStatus("users: "+msg.err.Error(), true)
		}
		// Keep the shell's tab completion current. A command, not a bare
		// goroutine, so it runs under the program (and not after a test).
		users, convs := msg.users, msg.convs
		save := func() tea.Msg { names.SaveTo(names.Path(), users, convs); return nil }
		return m, tea.Batch(cmd, m.refreshLater(), save, m.loadMe(), m.pollPresence(), m.loadScheduled())

	case convsMsg:
		if msg.err != nil {
			return m, m.setStatus(msg.err.Error(), true)
		}
		var fresh []string
		live := map[string]bool{}
		for _, c := range msg.convs {
			live[c.ID] = true
			if _, ok := m.convs[c.ID]; !ok {
				fresh = append(fresh, c.ID)
			}
			m.mergeConv(c)
		}
		var cmds []tea.Cmd
		for id := range m.convs {
			if !live[id] {
				cmds = append(cmds, m.dropConv(id)) // left or archived elsewhere
			}
		}
		m.syncPublic()
		m.rebuildSidebar()
		m.loadInfos(fresh)
		return m, tea.Batch(cmds...)

	case infoMsg:
		m.infoPending--
		if m.infoPending <= 0 {
			m.infoPending, m.infoTotal = 0, 0
		}
		if msg.err != nil {
			return m, tea.Batch(m.waitEvent(), m.setStatus(msg.err.Error(), true))
		}
		m.mergeConv(msg.conv)
		c := m.convs[msg.conv.ID]
		var cmds []tea.Cmd
		if c != nil && c.Unread() && m.current == unreadsID {
			if _, ok := m.unreadMsgs[c.ID]; !ok {
				cmds = append(cmds, m.loadSince(c.ID))
			}
		}
		if c != nil && !c.Unread() {
			delete(m.unreadMsgs, c.ID)
		}
		m.rebuildSidebar()
		return m, tea.Batch(append(cmds, m.waitEvent())...)

	case historyMsg:
		l := m.list(msg.id)
		l.loading = false
		if msg.err != nil {
			if j := m.jump; j != nil && j.conv == msg.id {
				m.jump = nil
			}
			return m, m.setStatus(msg.err.Error(), true)
		}
		if msg.older {
			if l.sel >= 0 {
				l.sel += len(msg.msgs)
			}
			l.msgs = append(msg.msgs, l.msgs...)
		} else {
			l.msgs, msg.hasMore = mergeFresh(l.msgs, msg.msgs, l.hasMore, msg.hasMore)
		}
		l.hasMore, l.loaded = msg.hasMore, true
		m.touch(msg.id)
		var jump tea.Cmd
		if j := m.jump; j != nil && j.thread == "" && j.conv == msg.id {
			jump = m.resolveJump()
		}
		if c := m.convs[msg.id]; c != nil && len(l.msgs) > 0 {
			for i := len(l.msgs) - 1; i >= 0; i-- {
				if !l.msgs[i].IsReply() {
					if backend.TSLess(c.Latest, l.msgs[i].TS) {
						c.Latest = l.msgs[i].TS
					}
					break
				}
			}
		}
		return m, jump

	case repliesMsg:
		if m.thread == nil || m.thread.conv != msg.id || m.thread.ts != msg.ts {
			return m, nil
		}
		m.thread.list.loading = false
		if msg.err != nil {
			return m, m.setStatus(msg.err.Error(), true)
		}
		// The fetched thread is complete; keep only live replies newer than it
		// (cached ones it no longer has were deleted).
		if n := len(msg.msgs); n > 0 {
			for _, x := range m.thread.list.msgs {
				if backend.TSLess(msg.msgs[n-1].TS, x.TS) && !m.thread.cached[x.TS] {
					msg.msgs = upsert(msg.msgs, x)
				}
			}
		} else {
			msg.msgs = m.thread.list.msgs
		}
		m.thread.list.msgs, m.thread.list.loaded = msg.msgs, true
		m.touch(msg.id)
		if j := m.jump; j != nil && j.thread != "" {
			return m, m.resolveJump()
		}
		return m, nil

	case sinceMsg:
		delete(m.fetchingUR, msg.id)
		if msg.err != nil {
			return m, m.setStatus(msg.err.Error(), true)
		}
		var top []backend.Message
		for _, x := range msg.msgs {
			if !x.IsReply() {
				top = append(top, x)
			}
		}
		if c := m.convs[msg.id]; c != nil && c.Unread() {
			m.unreadMsgs[msg.id] = top
		}
		return m, nil

	case postedMsg:
		if msg.err != nil {
			// Put the text back so it is not lost.
			if m.input.Value() == "" {
				m.input.SetValue(msg.text)
			}
			return m, m.setStatus("send failed: "+msg.err.Error(), true)
		}
		// Show it right away; the live event replaces it by timestamp.
		mine := backend.Message{TS: msg.ts, ThreadTS: msg.threadTS, User: m.client.SelfID, Text: msg.text}
		m.applyMessage(msg.id, mine)
		return m, nil

	case clipMsg:
		switch {
		case msg.err != nil:
			return m, m.setStatus("paste: "+msg.err.Error(), true)
		case msg.img != nil:
			if len(m.attach) >= maxAttach {
				return m, m.setStatus(fmt.Sprintf("at most %d images per message", maxAttach), true)
			}
			m.attach = append(m.attach, msg.img)
			if len(m.attach) == 1 {
				return m, m.setStatus("image attached — enter sends it", false)
			}
			return m, m.setStatus(fmt.Sprintf("%d images attached", len(m.attach)), false)
		}
		m.input.InsertString(msg.text)
		m.fitInput()
		m.updateCompletion()
		return m, nil

	case uploadedMsg:
		if msg.err != nil {
			// Keep the image and text so a retry is one enter away.
			if len(m.attach) == 0 {
				m.attach = msg.atts
			}
			if m.input.Value() == "" {
				m.input.SetValue(msg.text)
			}
			return m, m.setStatus("upload failed: "+msg.err.Error(), true)
		}
		return m, m.setStatus(plural(len(msg.atts), "sent 1 image", fmt.Sprintf("sent %d images", len(msg.atts))), false)

	case userInfoMsg:
		if msg.err == nil && msg.user.ID != "" {
			m.users[msg.user.ID] = msg.user
			// Not in users.list and from another workspace: Slack Connect.
			if t := msg.user.TeamID; t != "" && t != m.client.TeamID {
				m.extUsers[msg.user.ID] = true
			}
			m.rebuildSidebar() // DM names and their order may change
		}
		return m, nil

	case botMsg:
		if msg.err == nil && msg.name != "" {
			m.bots[msg.id] = msg.name
		}
		return m, nil

	case emojiMsg:
		if msg.err != nil {
			return m, m.setStatus("custom emoji: "+msg.err.Error(), true)
		}
		m.emoji = msg.emoji
		if m.cache != nil {
			m.cache.SaveEmoji(msg.emoji)
		}
		return m, nil

	case errMsg:
		return m, m.setStatus(msg.err.Error(), true)

	case meMsg:
		if msg.err != nil {
			return m, m.setStatus(msg.err.Error(), true)
		}
		m.away = msg.away
		return m, m.setDND(msg.dnd)

	case dndMsg:
		if msg.err != nil {
			return m, m.setStatus(msg.err.Error(), true)
		}
		cmd := m.setDND(msg.dnd)
		if until, on := msg.dnd.Until(time.Now()); on {
			return m, tea.Batch(cmd, m.setStatus("notifications paused until "+clock(until, m.cfg.TimeFormat), false))
		}
		return m, tea.Batch(cmd, m.setStatus("notifications on", false))

	case dndTick:
		if msg.seq != m.dndSeq {
			return m, nil
		}
		return m, m.reloadDND()

	case presenceMsg:
		for id, active := range msg.active {
			m.presence[id] = active
		}
		var cmd tea.Cmd
		if msg.poll {
			cmd = m.presenceLater()
		}
		if msg.err != nil {
			return m, tea.Batch(cmd, m.setStatus("presence: "+msg.err.Error(), true))
		}
		return m, cmd

	case presenceTick:
		return m, m.pollPresence()

	case statusSetMsg:
		return m, m.statusSet(msg)

	case awayMsg:
		if msg.err != nil {
			return m, m.setStatus(msg.err.Error(), true)
		}
		m.away = msg.away
		if msg.away {
			return m, m.setStatus("you show as away", false)
		}
		return m, m.setStatus("you show as active when Slack sees you", false)

	case markTick:
		if m.markSeq[msg.id] == msg.seq && m.current == msg.id {
			return m, m.markRead(msg.id)
		}
		return m, nil

	case refreshTick:
		var ids []string
		for _, c := range m.unreadConvs() {
			ids = append(ids, c.ID)
		}
		m.loadInfos(ids)
		return m, tea.Batch(m.refreshLater(), m.loadScheduled())

	case statusClear:
		if msg.seq == m.statusSeq {
			m.status = ""
		}
		return m, nil

	// --- live events ---
	case backend.EvConnection:
		m.conn = msg.State
		var cmd tea.Cmd
		if msg.Err != nil {
			cmd = m.setStatus(msg.Err.Error(), true)
		}
		return m, tea.Batch(cmd, m.waitEvent())

	case backend.EvMessage:
		return m, tea.Batch(m.onMessage(msg), m.waitEvent())

	case backend.EvMessageChanged:
		m.editMessage(msg.Channel, msg.Msg)
		return m, m.waitEvent()

	case backend.EvMessageDeleted:
		m.removeMessage(msg.Channel, msg.TS)
		return m, m.waitEvent()

	case backend.EvReaction:
		m.applyReaction(msg)
		return m, m.waitEvent()

	case backend.EvPin:
		return m, tea.Batch(m.pinEvent(msg), m.waitEvent())

	case backend.EvConversationsChanged:
		return m, tea.Batch(m.reloadConvs(), m.waitEvent())

	case backend.EvEmojiChanged:
		return m, tea.Batch(m.loadEmoji(), m.waitEvent())

	case backend.EvUserChanged:
		m.users[msg.User.ID] = msg.User
		return m, m.waitEvent()

	case backend.EvDND:
		return m, tea.Batch(m.setDND(msg.DND), m.waitEvent())
	}

	if m.insert {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// applyMessage adds a message to every view that shows it.
func (m *Model) applyMessage(convID string, msg backend.Message) {
	defer m.touch(convID)
	if msg.IsReply() || msg.SubType == "thread_broadcast" {
		if t := m.thread; t != nil && t.conv == convID && t.ts == msg.ThreadTS {
			t.list.msgs = upsert(t.list.msgs, msg)
		}
	}
	if msg.IsReply() {
		// Bump the parent's reply count in the channel view.
		if l, ok := m.lists[convID]; ok {
			for i := range l.msgs {
				if l.msgs[i].TS == msg.ThreadTS {
					key := convID + "/" + msg.TS
					if !m.seenReplies[key] {
						m.seenReplies[key] = true
						l.msgs[i].ReplyCount++
					}
					l.msgs[i].LatestReply = msg.TS
					break
				}
			}
		}
		return
	}
	if l, ok := m.lists[convID]; ok && l.loaded {
		l.msgs = upsert(l.msgs, msg)
	}
	if c := m.convs[convID]; c != nil && backend.TSLess(c.Latest, msg.TS) {
		c.Latest = msg.TS
	}
}

func (m *Model) onMessage(ev backend.EvMessage) tea.Cmd {
	c := m.convs[ev.Channel]
	if c == nil {
		// A conversation we don't know yet, e.g. a new DM.
		return m.reloadConvs()
	}
	msg := ev.Msg
	// Our own message from the post call may already be shown.
	m.applyMessage(ev.Channel, msg)

	if msg.User == m.client.SelfID {
		if !msg.IsReply() {
			c.LastRead = msg.TS
			c.Mentions = 0
			delete(m.unreadMsgs, ev.Channel)
		}
		m.rebuildSidebar()
		return nil
	}

	mentioned := strings.Contains(msg.Text, "<@"+m.client.SelfID+">")
	visible := m.current == ev.Channel
	if msg.IsReply() {
		// Thread replies don't make a channel unread.
		return nil
	}
	if visible && m.list(ev.Channel).sel < 0 {
		m.markSeq[ev.Channel]++
		seq := m.markSeq[ev.Channel]
		m.rebuildSidebar()
		return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return markTick{id: ev.Channel, seq: seq} })
	}
	if c.IsDM() || mentioned {
		c.Mentions++
	}
	if _, ok := m.unreadMsgs[ev.Channel]; ok {
		m.unreadMsgs[ev.Channel] = upsert(m.unreadMsgs[ev.Channel], msg)
	} else if m.current == unreadsID {
		m.unreadMsgs[ev.Channel] = []backend.Message{msg}
	}
	m.rebuildSidebar()
	return nil
}

func (m *Model) editMessage(convID string, msg backend.Message) {
	m.touch(convID)
	replace := func(msgs []backend.Message) {
		for i := range msgs {
			if msgs[i].TS == msg.TS {
				// A change event doesn't always carry everything: keep what
				// it leaves out. (Reactions only change through their own
				// events, so a missing list means "not included".)
				if msg.ReplyCount == 0 {
					msg.ReplyCount, msg.LatestReply = msgs[i].ReplyCount, msgs[i].LatestReply
				}
				if msg.Reactions == nil {
					msg.Reactions = msgs[i].Reactions
				}
				if msg.Files == nil {
					msg.Files = msgs[i].Files
				}
				if !msg.Pinned { // pins change through their own events too
					msg.Pinned = msgs[i].Pinned
				}
				msgs[i] = msg
				return
			}
		}
	}
	if l, ok := m.lists[convID]; ok {
		replace(l.msgs)
	}
	if t := m.thread; t != nil && t.conv == convID {
		replace(t.list.msgs)
	}
	if u, ok := m.unreadMsgs[convID]; ok {
		replace(u)
	}
}

func (m *Model) applyReaction(ev backend.EvReaction) {
	m.touch(ev.Channel)
	apply := func(msgs []backend.Message) {
		for i := range msgs {
			if msgs[i].TS != ev.TS {
				continue
			}
			rs := msgs[i].Reactions
			idx := -1
			for j, r := range rs {
				if r.Name == ev.Name {
					idx = j
				}
			}
			has := idx >= 0 && slices.Contains(rs[idx].Users, ev.User)
			if ev.Added == has {
				return // already applied (our own change, echoed back)
			}
			switch {
			case ev.Added && idx < 0:
				rs = append(rs, backend.Reaction{Name: ev.Name, Count: 1, Users: []string{ev.User}})
			case ev.Added:
				rs[idx].Count++
				rs[idx].Users = append(rs[idx].Users, ev.User)
			case idx >= 0:
				rs[idx].Count--
				users := rs[idx].Users[:0]
				for _, u := range rs[idx].Users {
					if u != ev.User {
						users = append(users, u)
					}
				}
				rs[idx].Users = users
				if rs[idx].Count <= 0 {
					rs = append(rs[:idx], rs[idx+1:]...)
				}
			}
			msgs[i].Reactions = rs
			return
		}
	}
	if l, ok := m.lists[ev.Channel]; ok {
		apply(l.msgs)
	}
	if t := m.thread; t != nil && t.conv == ev.Channel {
		apply(t.list.msgs)
	}
}

// mergeFresh folds the newest page from the API into what we already have.
// The fresh page wins for every message in its range, which repairs
// anything a missed live event left wrong; older pages loaded by scrolling
// are kept, and so are live messages newer than the page.
func mergeFresh(local, fresh []backend.Message, localMore, freshMore bool) ([]backend.Message, bool) {
	if len(fresh) == 0 {
		return local, localMore
	}
	first, last := fresh[0].TS, fresh[len(fresh)-1].TS
	// Older messages can only be kept when they connect to the page: with
	// more history behind it and no message in common (say, a cache from
	// days ago), there may be a gap in between. With no more history behind
	// it, older ones were deleted.
	if !freshMore || !slices.ContainsFunc(local, func(x backend.Message) bool {
		return !backend.TSLess(x.TS, first) && !backend.TSLess(last, x.TS)
	}) {
		local = slices.DeleteFunc(slices.Clone(local), func(x backend.Message) bool { return backend.TSLess(x.TS, first) })
	}
	var out []backend.Message
	keptOlder := false
	for _, x := range local {
		if backend.TSLess(x.TS, first) {
			out = append(out, x)
			keptOlder = true
		}
	}
	out = append(out, fresh...)
	for _, x := range local {
		if backend.TSLess(last, x.TS) {
			out = upsert(out, x)
		}
	}
	if keptOlder {
		return out, localMore
	}
	return out, freshMore
}
