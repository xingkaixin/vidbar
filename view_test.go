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
