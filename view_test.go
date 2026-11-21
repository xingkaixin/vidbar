package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xingkaixin/vidbar/internal/downloads"
)

func TestProgressShowsElapsedBytesAndSpeed(t *testing.T) {
	start := time.Unix(1000, 0)
	running := downloads.Item{Started: start, Done: 12_400_000, Total: 1_200_000_000, Speed: 2_100_000}
	if got, want := progress(running, start.Add(83*time.Second)), "01:23 · 12.4 MB / 1.2 GB · 2.1 MB/s"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := progress(downloads.Item{Started: start}, start), "00:00"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	merging := downloads.Item{Started: start, Done: 100, Total: 100, Speed: 50, Merging: true}
	if got, want := progress(merging, start.Add(83*time.Second)), "01:23 · MERGING"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Rows take sideways scrolls for their swipes, which must leave upright ones
// to the list.
func TestLongHistoryScrollsOverItsRows(t *testing.T) {
	var items []downloads.Item
	for i := range 30 {
		items = append(items, downloads.Item{ID: fmt.Sprint(i), Title: fmt.Sprint("video ", i), Status: downloads.Done, Time: time.Now()})
	}
	data, err := json.Marshal(map[string]any{"dir": "/videos", "items": items})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{queue: downloads.Open(path, "", func() {})}
	tt := ui.NewTester(a.view, 360, 460)
	tt.Wheel(180, 200, 0, 3000)
	tt.Advance(time.Second)
	if !tt.HasText("video 29") {
		t.Errorf("texts after scrolling %q", tt.Texts())
	}
}

func TestRowPopoverMenuRemovesRecordWithoutDeletingFile(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(video, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []downloads.Item{{
		ID:     "1",
		URL:    "https://example.com/v/1",
		Title:  "Sample Clip",
		Path:   video,
		Status: downloads.Done,
		Time:   time.Now(),
	}}
	data, err := json.Marshal(map[string]any{"dir": dir, "items": items})
	if err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(dir, "history.json")
	if err := os.WriteFile(hist, data, 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{queue: downloads.Open(hist, dir, func() {})}
	t.Cleanup(a.queue.Close)
	tt := ui.NewTester(a.view, 360, 460)

	if err := tt.RightClick("Sample Clip"); err != nil {
		t.Fatalf("right click: %v", err)
	}
	for _, want := range []string{"Show in Finder", "Quick Look", "Copy URL", "Remove from List", "Delete File"} {
		if !tt.HasText(want) {
			t.Errorf("missing menu item %q in %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Remove from List"); err != nil {
		t.Fatalf("click menu item: %v", err)
	}
	if len(a.queue.Items()) != 0 {
		t.Fatalf("expected item removed, got %+v", a.queue.Items())
	}
	if _, err := os.Stat(video); err != nil {
		t.Errorf("video file should remain on disk: %v", err)
	}
}

func TestMultilinePasteAndClipboardBar(t *testing.T) {
	if got := extractURLs("hello https://example.com/1"); got != nil {
		t.Fatalf("mixed prose should not extract URLs: %v", got)
	}
	dir := t.TempDir()
	a := &app{queue: downloads.Open(filepath.Join(dir, "history.json"), dir, func() {})}
	a.queue.Close()
	a.clipRaw = "https://example.com/one"
	a.clipURLs = a.clipboardURLs(a.clipRaw)

	tt := ui.NewTester(a.view, 360, 460)
	if !tt.HasText("example.com/one") {
		t.Fatalf("clipboard bar not shown: %q", tt.Texts())
	}
	if err := tt.Click("ADD"); err != nil {
		t.Fatalf("click ADD: %v", err)
	}
	if got := a.queue.Items(); len(got) != 1 || got[0].URL != "https://example.com/one" {
		t.Fatalf("unexpected items after ADD: %+v", got)
	}
	if len(a.clipboardURLs("https://example.com/one")) != 0 {
		t.Errorf("dismissed clipboard URL should not reappear")
	}

	if err := tt.Click("Video URL"); err != nil {
		t.Fatalf("focus input: %v", err)
	}
	tt.SetClipboard("https://example.com/a\nhttps://example.com/b\nhttps://example.com/a\n")
	tt.Command("paste")
	if got := a.queue.Items(); len(got) != 3 {
		t.Fatalf("expected 3 total items after multiline paste, got %d: %+v", len(got), got)
	}
	if a.url != "" {
		t.Errorf("multiline paste should not fill single-line input: %q", a.url)
	}
}
