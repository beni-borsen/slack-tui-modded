package tui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"

	"github.com/magnusdalkvist/slack-tui/internal/backend"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// Palette. Hand-picked; themes come later.
var (
	cFg      = lipgloss.Color("252")
	cDim     = lipgloss.Color("244")
	cFaint   = lipgloss.Color("238")
	cAccent  = lipgloss.Color("75")  // blue
	cUnread  = lipgloss.Color("231") // bright white
	cMention = lipgloss.Color("203") // red
	cSelf    = lipgloss.Color("221") // amber
	cCodeBg  = lipgloss.Color("236")
	cSelBg   = lipgloss.Color("237")
	cOK      = lipgloss.Color("114")
	cWarn    = lipgloss.Color("215")

	// cSelBg as raw SGR, for content that sets its own foreground.
	selBgOn  = "\x1b[48;5;237m"
	selBgOff = "\x1b[49m"

	nameColors = []lipgloss.Color{"75", "114", "176", "215", "80", "211", "150", "179", "141", "109"}
)

var (
	sDim       = lipgloss.NewStyle().Foreground(cDim)
	sFaint     = lipgloss.NewStyle().Foreground(cFaint)
	sNormal    = lipgloss.NewStyle().Foreground(cFg)
	sUnread    = lipgloss.NewStyle().Foreground(cUnread).Bold(true)
	sHeader    = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	sActive    = lipgloss.NewStyle().Foreground(cUnread).Background(cSelBg)
	sBadge     = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(cMention).Bold(true).Padding(0, 1)
	sTitle     = lipgloss.NewStyle().Foreground(cUnread).Bold(true)
	sTopic     = lipgloss.NewStyle().Foreground(cDim)
	sBar       = lipgloss.NewStyle().Foreground(cAccent)
	sBarDim    = lipgloss.NewStyle().Foreground(cFaint)
	sTime      = lipgloss.NewStyle().Foreground(cDim)
	sSystem    = lipgloss.NewStyle().Foreground(cDim).Italic(true)
	sNewLine   = lipgloss.NewStyle().Foreground(cMention)
	sDate      = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	sThread    = lipgloss.NewStyle().Foreground(cAccent)
	sReaction  = lipgloss.NewStyle().Foreground(cFg).Background(cCodeBg).Padding(0, 1)
	sReactSelf = lipgloss.NewStyle().Foreground(cUnread).Background(lipgloss.Color("24")).Padding(0, 1)
	sModeN     = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(cAccent).Bold(true).Padding(0, 1)
	sModeI     = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(cOK).Bold(true).Padding(0, 1)
	sOK        = lipgloss.NewStyle().Foreground(cOK)
	sWarn      = lipgloss.NewStyle().Foreground(cWarn)
	sErr       = lipgloss.NewStyle().Foreground(cMention)
	sBox       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
	sKey       = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
)

var mdStyles = render.Styles{
	Bold:         lipgloss.NewStyle().Bold(true),
	Italic:       lipgloss.NewStyle().Italic(true),
	Strike:       lipgloss.NewStyle().Strikethrough(true),
	Code:         lipgloss.NewStyle().Foreground(lipgloss.Color("217")).Background(cCodeBg),
	CodeBlock:    lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(cCodeBg),
	Quote:        lipgloss.NewStyle().Foreground(cDim),
	Link:         lipgloss.NewStyle().Foreground(cAccent).Underline(true),
	Mention:      lipgloss.NewStyle().Foreground(cAccent).Bold(true),
	SelfMention:  lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(cSelf).Bold(true),
	Channel:      lipgloss.NewStyle().Foreground(cAccent),
	UnknownEmoji: lipgloss.NewStyle().Foreground(cDim),
	CustomEmoji:  lipgloss.NewStyle().Foreground(lipgloss.Color("180")).Background(cCodeBg),
}

// senderKey is who a message is from, for its name colour and grouping: the
// person when there is one, since messages people send through an app (this
// one included) also carry the app's bot_id; otherwise the bot.
func senderKey(msg backend.Message) string {
	if msg.User != "" {
		return msg.User
	}
	return msg.BotName + msg.BotID
}

func nameStyle(userID string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(userID))
	return lipgloss.NewStyle().Foreground(nameColors[h.Sum32()%uint32(len(nameColors))]).Bold(true)
}
