package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/xingkaixin/mygo-native-ui/ui/swipeactions"

	"github.com/xingkaixin/vidbar/internal/downloads"
)

func icon(path string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="square" stroke-linejoin="miter"><path d="` + path + `"/></svg>`))
}

var (
	trashIcon = icon("M3.45 6.3h17.1M9.15 6.3V3.45h5.7v2.85M5.35 6.3l0.95 14.25h11.4l0.95-14.25M10.1 10.1v6.65m3.8-6.65v6.65")
	retryIcon = icon("M 3.963 4.856 v 5.358 h 5.358 m -5.358 0 a 8.037 8.037 0 0 1 14.288 -3.572 m 1.786 12.502 v -5.358 h -5.358 m 5.358 0 a 8.037 8.037 0 0 1 -14.288 3.572")
	closeIcon = icon("M6.3 6.3l11.4 11.4M17.7 6.3L6.3 17.7")
)

const (
	segments   = 32
	segmentGap = 3
)

func (a *app) view(c *ui.Context) {
	c.SetTheme(theme(c.Theme().Dark))
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	items := a.queue.Items()
	a.list.Key = func(row int) any { return items[row].ID }
	// The elapsed time moves on between yt-dlp's reports, and during a merge
	// there are none.
	if slices.ContainsFunc(items, func(it downloads.Item) bool { return it.Status == downloads.Running }) {
		c.After(time.Second)
	}

	ui.Column(c).Fill().Children(func() {
		input := ui.TextInput(c.Key("url"), &a.url).AutoFocus().Label("Video URL").Placeholder("Paste a video URL, press Enter").Font(fontMono).FontSize(12).Margin(14)
		if input.Submitted() {
			if url := strings.TrimSpace(a.url); url != "" {
				a.queue.Add(url)
			}
			a.url = ""
		}
		ui.Divider(c)
		ui.List(c, &a.list, len(items), func(i int) { a.row(c, items[i]) }).Grow(1).Label("Downloads").Children(func() {
			if len(items) == 0 {
				meta(c, "NO DOWNLOADS").Padding(32).AlignSelf(ui.Center)
			}
		})
		ui.Divider(c)
		ui.Row(c).Gap(8).Padding(8, 14).AlignItems(ui.Center).Children(func() {
			meta(c, shortPath(a.queue.Dir())).Grow(1).SingleLine()
			if ui.Button(c, "CHANGE").Label("Change download folder").Font(fontMono).FontSize(11).LetterSpacing(0.88).Clicked() {
				a.chooseDir()
			}
		})
	})
}

// meta is machine text: paths, times, sizes and states.
func meta(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).Font(fontMono).FontSize(11).LetterSpacing(0.88).TextColor(c.Theme().TextMuted)
}

func (a *app) row(c *ui.Context, it downloads.Item) {
	t := c.Theme()
	remove := swipeactions.Action{Label: "Delete", Icon: trashIcon, Color: t.Danger}
	retry := swipeactions.Action{Label: "Retry", Icon: retryIcon, Color: t.Accent}
	var actions []swipeactions.Action
	switch it.Status {
	case downloads.Queued, downloads.Running:
		actions = []swipeactions.Action{{Label: "Cancel", Icon: closeIcon, Color: t.Danger}}
	case downloads.Failed:
		actions = []swipeactions.Action{remove, retry}
	default:
		actions = []swipeactions.Action{remove}
	}

	ev := swipeactions.Row(c.Key(it.ID), &a.swipe, nil, actions, func() {
		ui.Row(c).Gap(10).Padding(10, 14).AlignItems(ui.Center).Children(func() {
			a.preview(c, it)
			ui.Column(c).Gap(5).Grow(1).Shrink(1).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					switch it.Status {
					case downloads.Running:
						led(c, t.Danger)
					case downloads.Failed:
						led(c, t.Accent)
					}
					ui.Text(c, it.Title).SingleLine().FontWeight(500).Grow(1).Shrink(1)
					if it.Status == downloads.Running {
						ui.Text(c, fmt.Sprintf("%d", int(it.Progress*100))).Font(fontDisplay).FontSize(20).LineHeight(1)
					}
				})
				if it.Status == downloads.Running {
					segbar(c, it.Progress)
					// No tracking: at this width the speed would be cut off.
					meta(c, progress(it, time.Now())).LetterSpacing(0).SingleLine()
					return
				}
				detail := meta(c, a.detail(it, time.Now())).MaxLines(1)
				if it.Status == downloads.Failed {
					detail.MaxLines(2)
				}
			})
		})
	})

	if action, ok := ev.Chosen(); ok {
		if action == retry {
			a.queue.Retry(it.ID)
		} else {
			a.remove(c, it)
		}
	}
	if ev.Clicked() && it.Status == downloads.Done && !a.missing[it.ID] {
		mygo.Clipboard.WriteText(it.Path)
		c.Toast("Path copied")
	}
}

// preview keeps its box when there is no picture, so that the titles line up.
func (a *app) preview(c *ui.Context, it downloads.Item) {
	if img := a.thumbnail(it.Thumbnail); img != nil {
		ui.Image(c, img).Size(64, 36).Shrink(0).Fit(ui.Cover).Label("Preview")
		return
	}
	ui.Box(c).Size(64, 36).Shrink(0).Background(c.Theme().Surface)
}

// thumbnail decodes a preview once: the view runs on every frame.
func (a *app) thumbnail(path string) ui.ImageSource {
	if path == "" {
		return nil
	}
	if img, ok := a.thumbnails[path]; ok {
		return img
	}
	var img ui.ImageSource
	data, err := os.ReadFile(path)
	if err == nil {
		img, err = ui.DecodeImage(data)
	}
	if err != nil {
		log.Printf("thumbnail: %v", err)
	}
	if a.thumbnails == nil {
		a.thumbnails = map[string]ui.ImageSource{}
	}
	a.thumbnails[path] = img
	return img
}

func led(c *ui.Context, color ui.Color) {
	ui.Box(c).Size(6, 6).Shrink(0).Radius(3).Background(color)
}

// segbar is the design's meter: sharp segments that light up from the left.
func segbar(c *ui.Context, value float64) {
	t := c.Theme()
	off := t.Text.Alpha(0.14)
	ui.Box(c).Height(6).Label("Download progress").Draw(func(p *ui.Painter, r ui.Rect) {
		w := (r.W - segmentGap*(segments-1)) / segments
		lit := int(value*segments + 0.5)
		for i := range segments {
			color := off
			if i < lit {
				color = t.Accent
			}
			p.Fill(ui.Rect{X: r.X + float32(i)*(w+segmentGap), Y: r.Y, W: w, H: r.H}, color, 0)
		}
	})
}

// progress is the state of a running download: elapsed, bytes and speed.
func progress(it downloads.Item, now time.Time) string {
	parts := []string{elapsed(now.Sub(it.Started))}
	if it.Done > 0 {
		bytes := size(it.Done)
		if it.Total > 0 {
			bytes += " / " + size(it.Total)
		}
		parts = append(parts, bytes)
	}
	if it.Speed > 0 {
		parts = append(parts, size(it.Speed)+"/s")
	}
	return strings.Join(parts, " · ")
}

func elapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func size(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e8:
		return fmt.Sprintf("%.0f MB", float64(n)/1e6)
	case n >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.0f KB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}

func (a *app) detail(it downloads.Item, now time.Time) string {
	switch {
	case it.Status == downloads.Queued:
		return "QUEUED"
	case it.Status == downloads.Failed:
		return it.Error
	case a.missing[it.ID]:
		return "FILE MISSING"
	}
	return relativeTime(it.Time, now) + " · " + filepath.Base(it.Path)
}

// remove trashes the file first, so that a record is never lost while its
// file stays on disk.
func (a *app) remove(c *ui.Context, it downloads.Item) {
	if it.Status == downloads.Done && !a.missing[it.ID] {
		if err := mygo.Shell.TrashItem(it.Path); err != nil {
			c.Toast("Could not delete: " + err.Error())
			return
		}
	}
	delete(a.thumbnails, it.Thumbnail)
	a.queue.Remove(it.ID)
}

func relativeTime(t, now time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	age := now.Sub(t)
	switch {
	case age < time.Minute:
		return "JUST NOW"
	case age < time.Hour:
		return fmt.Sprintf("%d MIN AGO", int(age.Minutes()))
	case t.After(today):
		return fmt.Sprintf("%d HR AGO", int(age.Hours()))
	case t.After(today.AddDate(0, 0, -1)):
		return "YESTERDAY"
	}
	return strings.ToUpper(t.Format("Jan 2"))
}

func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home); ok {
		return "~" + rest
	}
	return path
}
