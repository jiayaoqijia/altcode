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
	HeaderBg   lipgloss.Color // status bar background
	DiffAdd    lipgloss.Color // diff added lines
	DiffDel    lipgloss.Color // diff removed lines
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
	HeaderBg:   lipgloss.Color("#1A1E26"),
	DiffAdd:    lipgloss.Color("#10B981"),
	DiffDel:    lipgloss.Color("#EF4444"),
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
	HeaderBg:   lipgloss.Color("#1A1E26"),
	DiffAdd:    lipgloss.Color("#10B981"),
	DiffDel:    lipgloss.Color("#EF4444"),
}

// Dracula is the Dracula color scheme.
var Dracula = Theme{
	Name:       "dracula",
	Primary:    lipgloss.Color("#BD93F9"),
	Secondary:  lipgloss.Color("#8BE9FD"),
	Error:      lipgloss.Color("#FF5555"),
	Warning:    lipgloss.Color("#FFB86C"),
	Success:    lipgloss.Color("#50FA7B"),
	Muted:      lipgloss.Color("#6272A4"),
	Background: lipgloss.Color("#282A36"),
	Foreground: lipgloss.Color("#F8F8F2"),
	Border:     lipgloss.Color("#44475A"),
	HeaderBg:   lipgloss.Color("#1A1E26"),
	DiffAdd:    lipgloss.Color("#10B981"),
	DiffDel:    lipgloss.Color("#EF4444"),
}

// Nord is the Nord color scheme.
var Nord = Theme{
	Name:       "nord",
	Primary:    lipgloss.Color("#88C0D0"),
	Secondary:  lipgloss.Color("#81A1C1"),
	Error:      lipgloss.Color("#BF616A"),
	Warning:    lipgloss.Color("#EBCB8B"),
	Success:    lipgloss.Color("#A3BE8C"),
	Muted:      lipgloss.Color("#4C566A"),
	Background: lipgloss.Color("#2E3440"),
	Foreground: lipgloss.Color("#ECEFF4"),
	Border:     lipgloss.Color("#3B4252"),
	HeaderBg:   lipgloss.Color("#1A1E26"),
	DiffAdd:    lipgloss.Color("#10B981"),
	DiffDel:    lipgloss.Color("#EF4444"),
}

// TokyoNight is the Tokyo Night color scheme.
var TokyoNight = Theme{
	Name:       "tokyo-night",
	Primary:    lipgloss.Color("#7AA2F7"),
	Secondary:  lipgloss.Color("#7DCFFF"),
	Error:      lipgloss.Color("#F7768E"),
	Warning:    lipgloss.Color("#E0AF68"),
	Success:    lipgloss.Color("#9ECE6A"),
	Muted:      lipgloss.Color("#565F89"),
	Background: lipgloss.Color("#1A1B26"),
	Foreground: lipgloss.Color("#C0CAF5"),
	Border:     lipgloss.Color("#292E42"),
	HeaderBg:   lipgloss.Color("#1A1E26"),
	DiffAdd:    lipgloss.Color("#10B981"),
	DiffDel:    lipgloss.Color("#EF4444"),
}

// SolarizedDark is the Solarized Dark color scheme.
var SolarizedDark = Theme{
	Name:       "solarized-dark",
	Primary:    lipgloss.Color("#268BD2"),
	Secondary:  lipgloss.Color("#2AA198"),
	Error:      lipgloss.Color("#DC322F"),
	Warning:    lipgloss.Color("#B58900"),
	Success:    lipgloss.Color("#859900"),
	Muted:      lipgloss.Color("#586E75"),
	Background: lipgloss.Color("#002B36"),
	Foreground: lipgloss.Color("#839496"),
	Border:     lipgloss.Color("#073642"),
	HeaderBg:   lipgloss.Color("#1A1E26"),
	DiffAdd:    lipgloss.Color("#10B981"),
	DiffDel:    lipgloss.Color("#EF4444"),
}

// Themes maps theme names to their definitions.
var Themes = map[string]Theme{
	"default":          DefaultTheme,
	"catppuccin-mocha": CatppuccinMocha,
	"dracula":          Dracula,
	"nord":             Nord,
	"tokyo-night":      TokyoNight,
	"solarized-dark":   SolarizedDark,
}

// GetTheme returns the named theme, falling back to DefaultTheme.
func GetTheme(name string) Theme {
	if t, ok := Themes[name]; ok {
		return t
	}
	return DefaultTheme
}
