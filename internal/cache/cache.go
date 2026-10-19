// Package cache keeps what the client last saw in a local SQLite database,
// so the next start shows conversations, read markers and recent messages
// at once while the API calls catch up.
package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

// MaxMessages is how many of the newest messages are kept per list.
const MaxMessages = 200

// keepLists drops lists (mostly threads) nobody opened for this long.
const keepLists = 30 * 24 * time.Hour

const schemaVersion = 1

const schema = `
CREATE TABLE IF NOT EXISTS kv (
	key   TEXT PRIMARY KEY,
	value BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS lists (
	key      TEXT PRIMARY KEY, -- conversation ID, or "<conversation>/<thread ts>"
	has_more INTEGER NOT NULL,
	updated  INTEGER NOT NULL  -- unix seconds
);
CREATE TABLE IF NOT EXISTS messages (
	list TEXT NOT NULL,
	ts   TEXT NOT NULL,
	user TEXT NOT NULL,
	text TEXT NOT NULL,
	json BLOB NOT NULL,
	PRIMARY KEY (list, ts)
);
`

// Dir is where slack-tui keeps cached data.
func Dir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "slack-tui")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "slack-tui")
}

// Path is the database for one workspace.
func Path(teamID string) string { return filepath.Join(Dir(), "slack-"+teamID+".db") }

// RemoveAll deletes every workspace's database, for logout.
func RemoveAll() error {
	files, _ := filepath.Glob(filepath.Join(Dir(), "slack-*.db*"))
	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Cache writes in the background, one write at a time, so the UI never
// waits on the disk. Reads are only done at start and when a thread opens.
type Cache struct {
	db   *sql.DB
	ops  chan func(*sql.DB) error
	done chan struct{}
}

// Open opens (or creates) the database at path. A database from another
// schema version is thrown away: it is only a cache.
func Open(path string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := open(path)
	if err != nil {
		return nil, err
	}
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		db.Close()
		return nil, err
	}
	if v != schemaVersion {
		db.Close()
		for _, suffix := range []string{"", "-wal", "-shm"} {
			os.Remove(path + suffix)
		}
		if db, err = open(path); err != nil {
			return nil, err
		}
	}
	if _, err := db.Exec(schema + fmt.Sprintf("PRAGMA user_version = %d;", schemaVersion)); err != nil {
		db.Close()
		return nil, fmt.Errorf("cache schema: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	c := &Cache{db: db, ops: make(chan func(*sql.DB) error, 256), done: make(chan struct{})}
	go c.writer()
	c.prune()
	return c, nil
}

func open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// writer runs the queued writes. A failed write is dropped: the next
// session just starts with less cached.
func (c *Cache) writer() {
	defer close(c.done)
	for op := range c.ops {
		_ = op(c.db)
	}
}

// Close finishes the queued writes and closes the database.
func (c *Cache) Close() error {
	close(c.ops)
	<-c.done
	return c.db.Close()
}

func (c *Cache) queue(op func(*sql.DB) error) { c.ops <- op }

func (c *Cache) prune() {
	cutoff := time.Now().Add(-keepLists).Unix()
	c.queue(func(db *sql.DB) error {
		_, err := db.Exec(`DELETE FROM messages WHERE list IN (SELECT key FROM lists WHERE updated < ?);
			DELETE FROM lists WHERE updated < ?`, cutoff, cutoff)
		return err
	})
}

// --- writes -------------------------------------------------------------------

func (c *Cache) putKV(key string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.queue(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO kv(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, b)
		return err
	})
}

func (c *Cache) SaveUsers(us []backend.User)                { c.putKV("users", us) }
func (c *Cache) SaveConversations(cs []backend.Conversation) { c.putKV("conversations", cs) }
func (c *Cache) SaveEmoji(e map[string]string)               { c.putKV("emoji", e) }
func (c *Cache) SaveBots(b map[string]string)                { c.putKV("bots", b) }

// SaveList replaces what is stored for a list with msgs (oldest first).
// A conversation keeps its newest MaxMessages; a thread keeps its parent
// and its newest replies.
func (c *Cache) SaveList(key string, msgs []backend.Message, hasMore bool) {
	if len(msgs) > MaxMessages {
		if IsThread(key) {
			msgs = append(msgs[:1:1], msgs[len(msgs)-MaxMessages+1:]...)
		} else {
			msgs = msgs[len(msgs)-MaxMessages:]
			hasMore = true
		}
	}
	type row struct {
		ts, user, text string
		js             []byte
	}
	rows := make([]row, 0, len(msgs))
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		rows = append(rows, row{m.TS, m.User, m.Text, b})
	}
	now := time.Now().Unix()
	c.queue(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`DELETE FROM messages WHERE list = ?`, key); err != nil {
			return err
		}
		stmt, err := tx.Prepare(`INSERT INTO messages(list, ts, user, text, json) VALUES(?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, r := range rows {
			if _, err := stmt.Exec(key, r.ts, r.user, r.text, r.js); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO lists(key, has_more, updated) VALUES(?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET has_more = excluded.has_more, updated = excluded.updated`,
			key, hasMore, now); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// DeleteList forgets a list, e.g. a conversation the user left.
func (c *Cache) DeleteList(key string) {
	c.queue(func(db *sql.DB) error {
		_, err := db.Exec(`DELETE FROM messages WHERE list = ? OR list LIKE ? || '/%';
			DELETE FROM lists WHERE key = ? OR key LIKE ? || '/%'`, key, key, key, key)
		return err
	})
}

// IsThread tells thread list keys from conversation IDs.
func IsThread(key string) bool { return strings.Contains(key, "/") }

// ThreadKey is the list key for a thread.
func ThreadKey(conv, ts string) string { return conv + "/" + ts }

// --- reads --------------------------------------------------------------------

// List is a stored message list, oldest first.
type List struct {
	Msgs    []backend.Message
	HasMore bool
}

// Snapshot is everything needed to draw the client before the API answers.
type Snapshot struct {
	Users         []backend.User
	Conversations []backend.Conversation
	Emoji         map[string]string
	Bots          map[string]string
	Lists         map[string]List // conversations only; threads load when opened
}

// Empty is true when nothing was cached yet (first start).
func (s Snapshot) Empty() bool { return len(s.Conversations) == 0 }

// Load reads the snapshot. Unreadable pieces are skipped.
func (c *Cache) Load() (Snapshot, error) {
	var s Snapshot
	getKV := func(key string, v any) {
		var b []byte
		if c.db.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&b) == nil {
			_ = json.Unmarshal(b, v)
		}
	}
	getKV("users", &s.Users)
	getKV("conversations", &s.Conversations)
	getKV("emoji", &s.Emoji)
	getKV("bots", &s.Bots)

	s.Lists = map[string]List{}
	more := map[string]bool{}
	rows, err := c.db.Query(`SELECT key, has_more FROM lists WHERE key NOT LIKE '%/%'`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var k string
		var hm bool
		if rows.Scan(&k, &hm) == nil {
			more[k] = hm
		}
	}
	rows.Close()

	rows, err = c.db.Query(`SELECT list, json FROM messages WHERE list NOT LIKE '%/%' ORDER BY list, ts`)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var b []byte
		if rows.Scan(&k, &b) != nil {
			continue
		}
		var m backend.Message
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		l := s.Lists[k]
		l.Msgs = append(l.Msgs, m)
		l.HasMore = more[k]
		s.Lists[k] = l
	}
	return s, rows.Err()
}

// LoadList reads one list (e.g. a thread); ok is false when it isn't stored.
func (c *Cache) LoadList(key string) (List, bool) {
	var l List
	if c.db.QueryRow(`SELECT has_more FROM lists WHERE key = ?`, key).Scan(&l.HasMore) != nil {
		return l, false
	}
	rows, err := c.db.Query(`SELECT json FROM messages WHERE list = ? ORDER BY ts`, key)
	if err != nil {
		return l, false
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		var m backend.Message
		if rows.Scan(&b) == nil && json.Unmarshal(b, &m) == nil {
			l.Msgs = append(l.Msgs, m)
		}
	}
	return l, true
}
