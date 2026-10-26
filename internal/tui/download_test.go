package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

func TestSaveName(t *testing.T) {
	for in, want := range map[string]string{
		"plan.pdf": "plan.pdf", "../../etc/passwd": "_.._etc_passwd", ".bashrc": "bashrc", "": "F1", "..": "F1",
	} {
		if got := saveName(backend.File{ID: "F1", Name: in}); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestSaveFiles(t *testing.T) {
	m := fakeModel(t, 100, 24)
	dl := t.TempDir()
	m.cfg.DownloadDir = dl
	// Already in the download cache, so nothing hits the network.
	cached := func(id, name, body string) backend.File {
		dir := filepath.Join(cacheDir(), "files")
		os.MkdirAll(dir, 0o700)
		os.WriteFile(filepath.Join(dir, id+"-"+name), []byte(body), 0o600)
		return backend.File{ID: id, Name: name, URL: "https://files.slack.com/" + id}
	}
	os.WriteFile(filepath.Join(dl, "data.json"), []byte("mine"), 0o644)
	files := []backend.File{cached("F1", "data.json", `{"a":1}`), cached("F2", "notes", "hi")}

	msg := m.saveTo(files, m.downloadDir())().(savedMsg)
	saved := &msg
	if saved == nil || saved.err != nil {
		t.Fatalf("saved %+v", saved)
	}
	if strings.Join(saved.names, ",") != "data (1).json,notes" {
		t.Errorf("names %v", saved.names)
	}
	if b, _ := os.ReadFile(filepath.Join(dl, "data.json")); string(b) != "mine" {
		t.Error("an existing file was overwritten")
	}
	if b, _ := os.ReadFile(filepath.Join(dl, "data (1).json")); string(b) != `{"a":1}` {
		t.Errorf("saved %q", b)
	}
	m.Update(*saved)
	if !strings.HasPrefix(m.status, "saved 2 files to ") {
		t.Errorf("status %q", m.status)
	}

	m.Update(savedMsg{names: []string{"x.zip"}, dir: dl})
	if m.status != "saved x.zip to "+dl {
		t.Errorf("status %q", m.status)
	}
	if m.saveFiles(nil); m.status != "no file on this message" {
		t.Errorf("status %q", m.status)
	}
}
