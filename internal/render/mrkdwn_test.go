package render

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type fakeResolver struct{}

func (fakeResolver) UserName(id string) string {
	return map[string]string{"U1": "anna", "ME": "erik"}[id]
}
func (fakeResolver) ChannelName(id string) string     { return map[string]string{"C1": "general"}[id] }
func (fakeResolver) SelfID() string                   { return "ME" }
func (fakeResolver) EmojiImage(string) (string, bool) { return "", false }

func TestMrkdwnPlain(t *testing.T) {
	st := Styles{}
	for _, tc := range []struct{ in, want string }{
		{"hi <@U1> and <@ME>", "hi @anna and @erik"},
		{"see <#C1> or <#C2|random>", "see #general or #random"},
		{"<!here> deploy", "@here deploy"},
		{"<https://example.com/a_b_c|the docs> and <https://x.io/snake_case_path>", "the docs and https://x.io/snake_case_path"},
		{"*bold* _it_ ~gone~ `co_de_`", "bold it gone co_de_"},
		{"a *b* *c* d", "a b c d"},
		{"snake_case_word stays", "snake_case_word stays"},
		{"2 * 3 * 4", "2 * 3 * 4"},
		{"&lt;tag&gt; &amp; :+1: :thumbsup::skin-tone-2: :custom-thing:", "<tag> & 👍 👍 :custom-thing:"},
		{"&gt; quoted\nnot", "▎ quoted\nnot"},
		{":yay: :yay2: :partyparrot:", "🎉 🎉 :partyparrot:"},
		{"pre ```x := *y*``` post", "pre \n x := *y* \n post"},
		{"```only code```", " only code "},
	} {
		got := ansi.Strip(Mrkdwn(tc.in, fakeResolver{}, st))
		if got != tc.want {
			t.Errorf("Mrkdwn(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

func TestMrkdwnStyles(t *testing.T) {
	lipgloss.SetColorProfile(0) // TrueColor so styles emit escapes
	st := Styles{Bold: lipgloss.NewStyle().Bold(true)}
	if got := Mrkdwn("*x*", fakeResolver{}, st); got == "x" {
		t.Errorf("bold not applied: %q", got)
	}
}

func (fakeResolver) CustomEmoji(name string) (string, bool) {
	v, ok := map[string]string{"yay": "alias:tada", "yay2": "alias:yay", "partyparrot": "https://emoji.slack-edge.com/parrot.gif"}[name]
	return v, ok
}

func TestSanitizeWidth(t *testing.T) {
	old := inTmux
	defer func() { inTmux = old }()
	inTmux = true
	if got := sanitizeWidth("ok­soft 👍🏽"); got != "oksoft 👍" {
		t.Errorf("got %q", got)
	}
	inTmux = false
	if got := sanitizeWidth("👍🏽"); got != "👍🏽" {
		t.Errorf("outside tmux the skin tone should stay, got %q", got)
	}
}

func TestLinks(t *testing.T) {
	text := "see <https://github.com/x/y|github.com/x/y> and <https://a.dk/?q=1&amp;r=2|the docs>, <@U1>, <mailto:a@b.dk|a@b.dk>"
	if got := PlainLinks(text); got != "see https://github.com/x/y and the docs (https://a.dk/?q=1&r=2), <@U1>, a@b.dk (mailto:a@b.dk)" {
		t.Errorf("PlainLinks = %q", got)
	}
	if got := Links(text + " <https://github.com/x/y>"); len(got) != 2 || got[0] != (Link{"https://github.com/x/y", ""}) || got[1] != (Link{"https://a.dk/?q=1&r=2", "the docs"}) {
		t.Errorf("Links = %q", got)
	}
	if got := Hyperlink("https://x.dk", "x"); got != "\x1b]8;;https://x.dk\x1b\\x\x1b]8;;\x1b\\" {
		t.Errorf("Hyperlink = %q", got)
	}
	for _, bad := range []string{"https://x.dk/\x1b]52;c;aGk=\a", "javascript:alert(1)", "file:///etc/passwd"} {
		if got := Hyperlink(bad, "x"); got != "x" {
			t.Errorf("Hyperlink(%q) = %q, want plain text", bad, got)
		}
	}
}
