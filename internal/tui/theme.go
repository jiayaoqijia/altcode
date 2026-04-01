package tui

import "github.com/charmbracelet/lipgloss"

// Theme defines the color palette for the TUI.
type Theme struct {
	Name       string
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Error      lipgloss.Color
	Warning    lipgloss.Color
	Success    lipgloss.Color
	Muted      lipgloss.Color
	Background lipgloss.Color
	Foreground lipgloss.Color
	Border     lipgloss.Color
}

// DefaultTheme is a dark theme with purple accents.
var DefaultTheme = Theme{
	Name:       "default",
	Primary:    lipgloss.Color("#7C3AED"),
	Secondary:  lipgloss.Color("#06B6D4"),
	Error:      lipgloss.Color("#EF4444"),
	Warning:    lipgloss.Color("#F59E0B"),
	Success:    lipgloss.Color("#10B981"),
	Muted:      lipgloss.Color("#6B7280"),
	Background: lipgloss.Color(""),
	Foreground: lipgloss.Color(""),
	Border:     lipgloss.Color("#374151"),
}

// CatppuccinMocha is the Catppuccin Mocha palette.
var CatppuccinMocha = Theme{
	Name:       "catppuccin-mocha",
	Primary:    lipgloss.Color("#CBA6F7"),
	Secondary:  lipgloss.Color("#89DCEB"),
	Error:      lipgloss.Color("#F38BA8"),
	Warning:    lipgloss.Color("#FAB387"),
	Success:    lipgloss.Color("#A6E3A1"),
	Muted:      lipgloss.Color("#6C7086"),
	Background: lipgloss.Color("#1E1E2E"),
	Foreground: lipgloss.Color("#CDD6F4"),
	Border:     lipgloss.Color("#313244"),
}

// Themes maps theme names to their definitions.
var Themes = map[string]Theme{
	"default":          DefaultTheme,
	"catppuccin-mocha": CatppuccinMocha,
}

// GetTheme returns the named theme, falling back to DefaultTheme.
func GetTheme(name string) Theme {
	if t, ok := Themes[name]; ok {
		return t
	}
	return DefaultTheme
}
