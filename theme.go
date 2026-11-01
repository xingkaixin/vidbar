package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// The NULLFRAME design system's three families, each with one job.
const (
	fontDisplay = "Doto"
	fontMono    = "Space Mono"
	fontSans    = "Space Grotesk"
)

//go:embed resources/fonts/*.ttf
var fonts embed.FS

func registerFonts() error {
	for file, family := range map[string]string{
		"Doto-Regular.ttf":         fontDisplay,
		"SpaceMono-Regular.ttf":    fontMono,
		"SpaceMono-Bold.ttf":       fontMono,
		"SpaceGrotesk-Regular.ttf": fontSans,
		"SpaceGrotesk-Medium.ttf":  fontSans,
	} {
		data, err := fonts.ReadFile("resources/fonts/" + file)
		if err != nil {
			return err
		}
		if err := ui.RegisterFont(data, family); err != nil {
			return err
		}
	}
	return nil
}

var (
	darkTheme  = nullframe(ui.DarkTheme(), "#111111", "#1a1a1a", "#222222", "#333333", "#e8e8e8", "#999999", "#4a9e5c")
	lightTheme = nullframe(ui.LightTheme(), "#ffffff", "#f7f7f4", "#e6e6e1", "#d2d2cc", "#1a1a18", "#6c6c66", "#2f8a4c")
)

// nullframe turns t into the design system's theme. Red stays out of Accent:
// the design uses it once per screen.
func nullframe(t *ui.Theme, surface, raised, border, visible, text, muted, success string) *ui.Theme {
	orange := ui.Hex("#f26522")
	t.Background = ui.Hex(surface)
	t.Surface = ui.Hex(raised)
	t.SurfaceHover = ui.Hex(border)
	t.SurfacePressed = ui.Hex(visible)
	t.Border = ui.Hex(visible)
	t.Text = ui.Hex(text)
	t.TextMuted = ui.Hex(muted)
	t.Accent = orange
	t.AccentHover = orange.Mix(ui.Hex(text), 0.15)
	t.AccentPressed = orange.Mix(ui.Hex(surface), 0.15)
	t.AccentText = ui.Hex("#ffffff")
	t.Danger = ui.Hex("#d71921")
	t.Warning = orange
	t.Success = ui.Hex(success)
	t.Selection = orange.Alpha(0.3)
	t.Focus = orange.Alpha(0.6)
	t.Radius = 0
	t.Font = fontSans
	return t
}

func theme(dark bool) *ui.Theme {
	if dark {
		return darkTheme
	}
	return lightTheme
}
