package backend

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func blocksJSON(t *testing.T, text string) string {
	t.Helper()
	b := listBlocks(text)
	if b == nil {
		return ""
	}
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestListBlocksNone(t *testing.T) {
	for _, s := range []string{"hello", "a - b", "-not a list", "price: 5 - 3", "*bold* line"} {
		if b := listBlocks(s); b != nil {
			t.Errorf("%q should stay plain text", s)
		}
	}
}

func TestListBlocks(t *testing.T) {
	text := "Hej <@U1>! Changes:\n- `:join` and *bold _both_*\n- see <https://x.dk/a?b=1&amp;c=2|the docs> :tada:\n  - nested\n- back\n\nBye &lt;3"
	got := blocksJSON(t, text)
	for _, want := range []string{
		`{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Hej "},{"type":"user","user_id":"U1"},{"type":"text","text":"! Changes:"}]}`,
		`{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":":join","style":{"code":true}},{"type":"text","text":" and "},{"type":"text","text":"bold ","style":{"bold":true}},{"type":"text","text":"both","style":{"bold":true,"italic":true}}]}`,
		`{"type":"link","url":"https://x.dk/a?b=1\u0026c=2","text":"the docs"},{"type":"text","text":" "},{"type":"emoji","name":"tada"}`,
		`"style":"bullet","indent":1`,
		`{"type":"rich_text_section","elements":[{"type":"text","text":"Bye \u003c3"}]}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s\nin %s", want, got)
		}
	}
	if n := strings.Count(got, `"type":"rich_text_list"`); n != 3 {
		t.Errorf("%d lists, want 3 (outer, nested, outer again)", n)
	}
}

func TestListBlocksOrdered(t *testing.T) {
	got := blocksJSON(t, "1. one\n2. two\n   - sub\n3. three")
	if !strings.Contains(got, `"style":"ordered","indent":0,"border":0,"offset":2`) {
		t.Errorf("numbering should continue after the nested list: %s", got)
	}
}

func TestListBlocksCodeAndQuote(t *testing.T) {
	got := blocksJSON(t, "- item\n```\nx := a &amp;&amp; b\n- not a list\n```\n&gt; quoted *line*")
	for _, want := range []string{
		`{"type":"rich_text_preformatted","elements":[{"type":"text","text":"x := a \u0026\u0026 b\n- not a list"}]`,
		`{"type":"rich_text_quote","elements":[{"type":"text","text":"quoted "},{"type":"text","text":"line","style":{"bold":true}}]}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s\nin %s", want, got)
		}
	}
}

func TestInlineEdges(t *testing.T) {
	for in, want := range map[string]string{
		"snake_case_name":   `[{"type":"text","text":"snake_case_name"}]`,
		"kl. 10:30: ok":     `[{"type":"text","text":"kl. 10:30: ok"}]`,
		"2*3*4":             `[{"type":"text","text":"2*3*4"}]`,
		"a * b * c":         `[{"type":"text","text":"a * b * c"}]`,
		"<!here> <#C9|dev>": `[{"type":"broadcast","range":"here"},{"type":"text","text":" "},{"type":"channel","channel_id":"C9"}]`,
		"`unclosed":         `[{"type":"text","text":"` + "`" + `unclosed"}]`,
	} {
		b, _ := json.Marshal(inline(in, nil))
		if string(b) != want {
			t.Errorf("%q:\n got %s\nwant %s", in, b, want)
		}
	}
}

func TestListFallback(t *testing.T) {
	if got := listFallback("Hej\n- a\n  * b\n1. c"); got != "Hej\n• a\n  • b\n1. c" {
		t.Errorf("%q", got)
	}
}

func TestBlocksText(t *testing.T) {
	// As Slack stored a list message (live): its text was flattened.
	raw := `{"type":"message","ts":"1.0","user":"U1","text":"test: • ` + "`:join`" + ` &amp; *bold* • second   • nested end","blocks":[{"type":"rich_text","block_id":"mYnYk","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"test:"}]},{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":":join","style":{"code":true}},{"type":"text","text":" & "},{"type":"text","text":"bold ","style":{"bold":true}},{"type":"link","url":"https://x.dk","text":"docs"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"second"},{"type":"emoji","name":"tada"}]}],"style":"bullet","indent":0,"offset":0,"border":0},{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"U2"}]}],"style":"ordered","indent":1,"offset":2,"border":0},{"type":"rich_text_section","elements":[{"type":"text","text":"end\n"}]}]}]}`
	var m slack.Msg
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	want := "test:\n• `:join` &amp; *bold* <https://x.dk|docs>\n• second:tada:\n  3. <@U2>\nend"
	if got := fromMsg(m).Text; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Without a list the text is used as it is.
	m.Blocks = slack.Blocks{}
	if got := fromMsg(m).Text; got != m.Text {
		t.Errorf("got %q", got)
	}
}

func TestListRoundTrip(t *testing.T) {
	text := "Hej!\n- `:join` og *fed*\n- <https://x.dk|link> :tada:\n  1. under\nslut"
	b, _ := json.Marshal(listBlocks(text))
	var blocks slack.Blocks
	if err := json.Unmarshal(b, &blocks); err != nil {
		t.Fatal(err)
	}
	got, ok := blocksText(blocks)
	if want := "Hej!\n• `:join` og *fed*\n• <https://x.dk|link> :tada:\n  1. under\nslut"; !ok || got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
