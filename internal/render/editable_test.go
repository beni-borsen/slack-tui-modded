package render

import "testing"

func TestEditableRoundTrip(t *testing.T) {
	for _, in := range []string{
		"hi <@U1> see <#C1> &amp; <#C2|random>",
		"<!here> deploy &lt;now&gt;",
		"docs: <https://x.io/a?b=1&amp;c=2|the docs> and <https://y.io>",
		"plain text with *bold* and `code`",
		"<@U404|ghost> stays",
	} {
		ed, tok := Editable(in, fakeResolver{})
		out := Encode(ed, tok)
		want := in
		if in == "docs: <https://x.io/a?b=1&amp;c=2|the docs> and <https://y.io>" {
			want = "docs: <https://x.io/a?b=1&amp;c=2|the docs> and https://y.io" // bare link becomes plain URL
		}
		if in == "<@U404|ghost> stays" {
			want = in
		}
		if out != want {
			t.Errorf("round trip\n in  %q\n ed  %q\n out %q\nwant %q", in, ed, out, want)
		}
	}
	ed, _ := Editable("hi <@U1> &amp; <#C1>", fakeResolver{})
	if ed != "hi @anna & #general" {
		t.Errorf("editable form = %q", ed)
	}
	if got := Encode("a < b && c > d", nil); got != "a &lt; b &amp;&amp; c &gt; d" {
		t.Errorf("escape: %q", got)
	}
}
