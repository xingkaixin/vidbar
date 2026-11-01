package ytdlp

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestReadFoldsMergedStreamsIntoOneProgress(t *testing.T) {
	output := `{"title":"Clip","formats":"299+258"}
{"format":"299","done":50,"total":100}
{"format":"299","done":100,"total":100}
{"format":"258","done":50,"total":100}
[Merger] not a line of ours
{"path":"/videos/clip.mp4"}
`
	var fractions []float64
	path := read(strings.NewReader(output), func(u Update) {
		if u.Title != "Clip" {
			t.Errorf("title %q", u.Title)
		}
		fractions = append(fractions, u.Fraction)
	})
	if path != "/videos/clip.mp4" {
		t.Errorf("path %q", path)
	}
	if want := []float64{0, 0.25, 0.5, 0.75}; !slices.Equal(fractions, want) {
		t.Errorf("fractions %v, want %v", fractions, want)
	}
}

func TestReadCountsBytesAcrossStreams(t *testing.T) {
	output := `{"title":"Clip","formats":"299+258","size":200}
{"format":"299","done":150,"total":150,"speed":30}
{"format":"258","done":25,"total":40,"speed":10}
`
	var last Update
	read(strings.NewReader(output), func(u Update) { last = u })
	want := Update{Title: "Clip", Fraction: 175.0 / 190, Done: 175, Total: 190, Speed: 10}
	if last != want {
		t.Errorf("update %+v, want %+v", last, want)
	}
}

func TestFailurePrefersTheLastYtDlpError(t *testing.T) {
	stderr := "WARNING: slow\nERROR: first\nERROR: Video unavailable\nWARNING: bye\n"
	if got := failure(stderr, errors.New("exit status 1")).Error(); got != "Video unavailable" {
		t.Errorf("got %q", got)
	}
	if got := failure("", errors.New("exit status 1")).Error(); got != "exit status 1" {
		t.Errorf("got %q", got)
	}
}
