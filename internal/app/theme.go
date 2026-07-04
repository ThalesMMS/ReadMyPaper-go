package app

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	colorAccent           = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0xff} // indigo-600
	colorAccentForeground = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colorAccentFocus      = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0x33}
	colorAccentSelection  = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0x40}
	colorAccentHover      = color.NRGBA{R: 0x4f, G: 0x46, B: 0xe5, A: 0x18}
)

// vividTheme layers a refined indigo accent, softer rounded corners and a
// little extra breathing room on top of Fyne's built-in theme.
type vividTheme struct{}

// NewTheme returns the ReadMyPaper visual theme.
func NewTheme() fyne.Theme { return vividTheme{} }

func (vividTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return colorAccent
	case theme.ColorNameForegroundOnPrimary:
		return colorAccentForeground
	case theme.ColorNameFocus:
		return colorAccentFocus
	case theme.ColorNameSelection:
		return colorAccentSelection
	case theme.ColorNameHover:
		return colorAccentHover
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (vividTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (vividTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (vividTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 11
	case theme.SizeNameInputRadius:
		return 10
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBarRadius:
		return 6
	case theme.SizeNameHeadingText:
		return 26
	case theme.SizeNameSubHeadingText:
		return 19
	}
	return theme.DefaultTheme().Size(name)
}

// withAlpha returns c with its alpha channel replaced, for translucent tints
// (hero banners, status chips) that read correctly in both theme variants.
func withAlpha(c color.Color, alpha uint8) color.NRGBA {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: alpha}
}
