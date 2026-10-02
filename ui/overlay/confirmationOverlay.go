package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConfirmationOverlay represents a confirmation dialog overlay
type ConfirmationOverlay struct {
	// Whether the overlay has been dismissed
	Dismissed bool
	// Message to display in the overlay
	message string
	// Width of the overlay
	width int
	// Callback function to be called when the user confirms (presses 'y')
	OnConfirm func()
	// Callback function to be called when the user cancels (presses 'n' or 'esc')
	OnCancel func()
	// Custom confirm key (defaults to 'y')
	ConfirmKey string
	// Custom cancel key (defaults to 'n')
	CancelKey string
	// ConfirmLabel names the confirm button (defaults to "Yes")
	ConfirmLabel string
	// Custom styling options
	borderColor lipgloss.Color
}

// NewConfirmationOverlay creates a new confirmation dialog overlay with the given message
func NewConfirmationOverlay(message string) *ConfirmationOverlay {
	return &ConfirmationOverlay{
		Dismissed:    false,
		message:      message,
		width:        50, // Default width
		ConfirmKey:   "y",
		ConfirmLabel: "Yes",
		CancelKey:    "n",
		borderColor:  lipgloss.Color("#de613e"), // Red color for confirmations
	}
}

// HandleKeyPress processes a key press and updates the state
// Returns true if the overlay should be closed
func (c *ConfirmationOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	switch msg.String() {
	case c.ConfirmKey, "enter": // Enter presses the focused (confirm) button
		c.Dismissed = true
		if c.OnConfirm != nil {
			c.OnConfirm()
		}
		return true
	case c.CancelKey, "esc":
		c.Dismissed = true
		if c.OnCancel != nil {
			c.OnCancel()
		}
		return true
	default:
		// Ignore other keys in confirmation state
		return false
	}
}

// Render draws the dialog: a filled box with the question and two buttons,
// the confirm one highlighted as the default for Enter.
func (c *ConfirmationOverlay) Render(opts ...WhitespaceOption) string {
	bg := lipgloss.Color("#2a2a3c")
	inner := max(c.width-6, 10) // border and two columns of padding each side
	base := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#e6e6e6"))
	key := base.Foreground(lipgloss.Color("#888888"))

	message := base.Width(inner).Render(c.message)
	confirm := lipgloss.NewStyle().Background(c.borderColor).Foreground(lipgloss.Color("#ffffff")).Bold(true).
		Render(" " + c.ConfirmLabel + " ⏎ ")
	cancel := lipgloss.NewStyle().Background(lipgloss.Color("#44445a")).Foreground(lipgloss.Color("#e6e6e6")).
		Render(" Cancel esc ")
	buttons := cancel + base.Render("  ") + confirm
	buttons = base.Width(inner).Align(lipgloss.Right).Render(buttons)
	hint := key.Width(inner).Render(c.ConfirmKey + " / Enter confirms · " + c.CancelKey + " / esc cancels")
	gap := base.Width(inner).Render("")

	content := lipgloss.JoinVertical(lipgloss.Left, message, gap, buttons, gap, hint)
	return lipgloss.NewStyle().
		Border(lipgloss.ThickBorder()).
		BorderForeground(c.borderColor).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 2).
		Render(content)
}

// SetWidth sets the width of the confirmation overlay
func (c *ConfirmationOverlay) SetWidth(width int) {
	c.width = width
}

// SetBorderColor sets the border color of the confirmation overlay
func (c *ConfirmationOverlay) SetBorderColor(color lipgloss.Color) {
	c.borderColor = color
}

// SetConfirmKey sets the key used to confirm the action
func (c *ConfirmationOverlay) SetConfirmKey(key string) {
	c.ConfirmKey = key
}

// SetCancelKey sets the key used to cancel the action
func (c *ConfirmationOverlay) SetCancelKey(key string) {
	c.CancelKey = key
}
