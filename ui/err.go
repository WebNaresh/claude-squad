package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type ErrBox struct {
	height, width int
	err           error
}

var errStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#c0392b",
	Dark:  "#e06c75",
})

func NewErrBox() *ErrBox {
	return &ErrBox{}
}

func (e *ErrBox) SetError(err error) {
	e.err = err
}

func (e *ErrBox) Clear() {
	e.err = nil
}

func (e *ErrBox) SetSize(width, height int) {
	e.width = width
	e.height = height
}

func (e *ErrBox) String() string {
	var err string
	if e.err != nil {
		err = e.err.Error()
		lines := strings.Split(err, "\n")
		err = strings.Join(lines, "//")
		if runewidth.StringWidth(err) > e.width-3 && e.width-3 >= 0 {
			err = runewidth.Truncate(err, e.width-3, "...")
		}
	}
	// Left-aligned under the key bar, so the eye finds it in the same place.
	return lipgloss.Place(e.width, e.height, lipgloss.Left, lipgloss.Center, " "+errStyle.Render(err))
}
