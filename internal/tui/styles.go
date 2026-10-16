package tui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"

	"github.com/magnusdalkvist/slack-tui/internal/config"
	"github.com/magnusdalkvist/slack-tui/internal/render"
)

// Palette, set by ApplyTheme.
var (
	cFg, cDim, cFaint, cAccent, cUnread, cMention, cSelf lipgloss.Color
	cCodeBg, cSelBg, cOK, cWarn                          lipgloss.Color

	nameColors []lipgloss.Color
)

var (
	sDim, sFaint, sNormal, sUnread, sHeader, sActive, sCursor, sBadge lipgloss.Style
	sTitle, sTopic, sBar, sBarDim, sTime, sSystem, sNewLine, sDate    lipgloss.Style
	sThread, sReaction, sReactSelf, sStatusBar, sModeN, sModeI, sOK   lipgloss.Style
	sWarn, sErr, sBox, sKey                                           lipgloss.Style

	mdStyles render.Styles
)

func init() { ApplyTheme(config.DefaultTheme()) }

// ApplyTheme sets the palette and rebuilds every style from it. Call it before
// the program starts drawing.
func ApplyTheme(t config.Theme) {
	c := func(s string) lipgloss.Color { return lipgloss.Color(s) }
	cFg = c(t.Fg)
	cDim = c(t.Dim)
	cFaint = c(t.Faint)
	cAccent = c(t.Accent)
	cUnread = c(t.Unread)
	cMention = c(t.Mention)
	cSelf = c(t.Self)
	cCodeBg = c(t.CodeBg)
	cSelBg = c(t.SelectionBg)
	cOK = c(t.OK)
	cWarn = c(t.Warn)
	contrast := c(t.ContrastFg)

	nameColors = nameColors[:0]
	for _, n := range t.Names {
		nameColors = append(nameColors, c(n))
	}

	sDim = lipgloss.NewStyle().Foreground(cDim)
	sFaint = lipgloss.NewStyle().Foreground(cFaint)
	sNormal = lipgloss.NewStyle().Foreground(cFg)
	sUnread = lipgloss.NewStyle().Foreground(cUnread).Bold(true)
	sHeader = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	sActive = lipgloss.NewStyle().Foreground(cUnread).Background(cSelBg)
	sCursor = lipgloss.NewStyle().Background(cSelBg)
	sBadge = lipgloss.NewStyle().Foreground(c(t.BadgeFg)).Background(cMention).Bold(true).Padding(0, 1)
	sTitle = lipgloss.NewStyle().Foreground(cUnread).Bold(true)
	sTopic = lipgloss.NewStyle().Foreground(cDim)
	sBar = lipgloss.NewStyle().Foreground(cAccent)
	sBarDim = lipgloss.NewStyle().Foreground(cFaint)
	sTime = lipgloss.NewStyle().Foreground(cDim)
	sSystem = lipgloss.NewStyle().Foreground(cDim).Italic(true)
	sNewLine = lipgloss.NewStyle().Foreground(cMention)
	sDate = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	sThread = lipgloss.NewStyle().Foreground(cAccent)
	sReaction = lipgloss.NewStyle().Foreground(cFg).Background(cCodeBg).Padding(0, 1)
	sReactSelf = lipgloss.NewStyle().Foreground(cUnread).Background(c(t.ReactionSelfBg)).Padding(0, 1)
	sStatusBar = lipgloss.NewStyle().Foreground(cDim)
	sModeN = lipgloss.NewStyle().Foreground(contrast).Background(cAccent).Bold(true).Padding(0, 1)
	sModeI = lipgloss.NewStyle().Foreground(contrast).Background(cOK).Bold(true).Padding(0, 1)
	sOK = lipgloss.NewStyle().Foreground(cOK)
	sWarn = lipgloss.NewStyle().Foreground(cWarn)
	sErr = lipgloss.NewStyle().Foreground(cMention)
	sBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
	sKey = lipgloss.NewStyle().Foreground(cAccent).Bold(true)

	mdStyles = render.Styles{
		Bold:         lipgloss.NewStyle().Bold(true),
		Italic:       lipgloss.NewStyle().Italic(true),
		Strike:       lipgloss.NewStyle().Strikethrough(true),
		Code:         lipgloss.NewStyle().Foreground(c(t.CodeFg)).Background(cCodeBg),
		CodeBlock:    lipgloss.NewStyle().Foreground(c(t.CodeBlockFg)).Background(cCodeBg),
		Quote:        lipgloss.NewStyle().Foreground(cDim),
		Link:         lipgloss.NewStyle().Foreground(cAccent).Underline(true),
		Mention:      lipgloss.NewStyle().Foreground(cAccent).Bold(true),
		SelfMention:  lipgloss.NewStyle().Foreground(contrast).Background(cSelf).Bold(true),
		Channel:      lipgloss.NewStyle().Foreground(cAccent),
		UnknownEmoji: lipgloss.NewStyle().Foreground(cDim),
		CustomEmoji:  lipgloss.NewStyle().Foreground(c(t.CustomEmoji)).Background(cCodeBg),
	}
}

func nameStyle(userID string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(userID))
	return lipgloss.NewStyle().Foreground(nameColors[h.Sum32()%uint32(len(nameColors))]).Bold(true)
}
