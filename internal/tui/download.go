package tui

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
)

// `s` saves files to the Downloads folder (or download_dir), next to
// whatever is there: plan.pdf, plan (1).pdf, …

type savedMsg struct {
	names []string // as saved, after renaming to avoid clashes
	dir   string
	err   error
}

func (m *Model) downloadDir() string {
	home, _ := os.UserHomeDir()
	if d := m.cfg.DownloadDir; d != "" {
		if d == "~" || strings.HasPrefix(d, "~/") {
			d = filepath.Join(home, d[1:])
		}
		return d
	}
	if out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output(); err == nil {
		// It answers $HOME when no Downloads folder is set up.
		if d := strings.TrimSpace(string(out)); d != "" && d != home {
			return d
		}
	}
	return filepath.Join(home, "Downloads")
}

// saveFiles downloads files (through the cache `o` uses) into the
// download folder.
func (m *Model) saveFiles(files []backend.File) tea.Cmd {
	if len(files) == 0 {
		return m.setStatus("no file on this message", false)
	}
	dir := m.downloadDir()
	what := saveName(files[0])
	if len(files) > 1 {
		what = fmt.Sprintf("%d files", len(files))
	}
	return tea.Batch(m.setStatus("saving "+what+"…", false), m.saveTo(files, dir))
}

func (m *Model) saveTo(files []backend.File, dir string) tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return savedMsg{dir: dir, err: err}
		}
		var names []string
		for _, f := range files {
			src, err := m.cachedFile(f)
			if err != nil {
				return savedMsg{names: names, dir: dir, err: fmt.Errorf("%s: %w", saveName(f), err)}
			}
			name, err := copyNew(src, dir, saveName(f))
			if err != nil {
				return savedMsg{names: names, dir: dir, err: fmt.Errorf("%s: %w", saveName(f), err)}
			}
			names = append(names, name)
		}
		return savedMsg{names: names, dir: dir}
	}
}

// saveName is the file's own name, made safe to put in a folder: no
// directories, not hidden.
func saveName(f backend.File) string {
	name := strings.NewReplacer("/", "_", "\\", "_", "\x00", "").Replace(f.Name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" {
		name = f.ID
	}
	return name
}

// copyNew copies src into dir as name, or "name (1).ext" and so on when
// that is taken; it never overwrites. It returns the name used.
func copyNew(src, dir, name string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	ext := filepath.Ext(name)
	if ext == name {
		ext = ""
	}
	base := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		try := name
		if i > 0 {
			try = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		path := filepath.Join(dir, try)
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(path)
			return "", err
		}
		return try, out.Close()
	}
	return "", errors.New("too many files with that name")
}

func (m *Model) savedResult(msg savedMsg) tea.Cmd {
	dir := msg.dir
	if home, _ := os.UserHomeDir(); home != "" && strings.HasPrefix(dir, home+"/") {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	if msg.err != nil {
		return m.setStatus("couldn't save "+msg.err.Error(), true)
	}
	what := msg.names[0]
	if len(msg.names) > 1 {
		what = fmt.Sprintf("%d files", len(msg.names))
	}
	return m.setStatus("saved "+what+" to "+dir, false)
}
