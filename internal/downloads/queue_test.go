package downloads

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xingkaixin/vidbar/internal/ytdlp"
)

func open(t *testing.T, fetch fetchFunc) *Queue {
	t.Helper()
	q := Open(filepath.Join(t.TempDir(), "history.json"), "/videos", func() {})
	q.fetch = fetch
	t.Cleanup(q.Close)
	return q
}

func TestDownloadsRunOneAtATimeOldestFirst(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string)
	q := open(t, func(_ context.Context, url, _, _ string, _ func(ytdlp.Update)) (string, error) {
		started <- url
		<-release
		return "", errors.New("gone")
	})
	q.Add("first")
	q.Add("second")
	if url := <-started; url != "first" {
		t.Fatalf("started %q", url)
	}
	release <- struct{}{}
	if url := <-started; url != "second" {
		t.Fatalf("started %q", url)
	}
	release <- struct{}{}
}

func TestRemoveStopsTheRunningDownload(t *testing.T) {
	started := make(chan struct{})
	q := open(t, func(ctx context.Context, _, _, _ string, _ func(ytdlp.Update)) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	q.Add("video")
	<-started
	q.Remove(q.Items()[0].ID)
	q.Close()
	if items := q.Items(); len(items) != 0 {
		t.Errorf("items %+v", items)
	}
}

func TestFailedDownloadCanBeRetried(t *testing.T) {
	file := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	finished := make(chan struct{})
	q := open(t, func(context.Context, string, string, string, func(ytdlp.Update)) (string, error) {
		defer func() { finished <- struct{}{} }()
		attempts++
		if attempts == 1 {
			return "", errors.New("network")
		}
		return file, nil
	})
	q.Add("video")
	<-finished
	waitFor(t, q, Failed)
	q.Retry(q.Items()[0].ID)
	<-finished
	if it := waitFor(t, q, Done); it.Path != file || it.Size != 4 || it.Error != "" {
		t.Errorf("item %+v", it)
	}
}

func TestOpenReportsUnfinishedDownloadsAsInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	saved := `{"dir":"/videos","items":[{"id":"a","status":"running"},{"id":"b","status":"queued"},{"id":"c","status":"done"}]}`
	if err := os.WriteFile(path, []byte(saved), 0o644); err != nil {
		t.Fatal(err)
	}
	items := Open(path, "/default", func() {}).Items()
	want := []Status{Failed, Failed, Done}
	for i, it := range items {
		if it.Status != want[i] {
			t.Errorf("item %s is %s", it.ID, it.Status)
		}
	}
}

func TestStartNotifiesAndCancelCleansOrphanThumbnail(t *testing.T) {
	started := make(chan string, 1)
	runningSeen := make(chan struct{}, 1)
	var q *Queue
	q = Open(filepath.Join(t.TempDir(), "history.json"), "/videos", func() {
		for _, it := range q.Items() {
			if it.Status == Running {
				select {
				case runningSeen <- struct{}{}:
				default:
				}
			}
		}
	})
	t.Cleanup(q.Close)

	finished := make(chan Item, 1)
	q.OnFinish(func(it Item) { finished <- it })
	q.fetch = func(ctx context.Context, _, _, thumb string, _ func(ytdlp.Update)) (string, error) {
		started <- thumb
		<-ctx.Done()
		_ = os.MkdirAll(filepath.Dir(thumb), 0o755)
		_ = os.WriteFile(thumb, []byte("jpg"), 0o644)
		return "", ctx.Err()
	}

	q.Add("video")
	thumb := <-started
	select {
	case <-runningSeen:
	case <-time.After(time.Second):
		t.Fatal("onChange did not observe Running before first progress update")
	}
	q.Remove(q.Items()[0].ID)
	q.Close()
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Errorf("orphan thumbnail still exists: %v", err)
	}
	select {
	case it := <-finished:
		t.Errorf("unexpected OnFinish for canceled item: %+v", it)
	default:
	}
}

func TestRetryRequeuesDoneItemAndFiresOnFinish(t *testing.T) {
	file := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan Item, 2)
	q := open(t, func(context.Context, string, string, string, func(ytdlp.Update)) (string, error) {
		return file, nil
	})
	q.OnFinish(func(it Item) { done <- it })
	q.Add("video")
	first := <-done
	if first.Status != Done || first.Path != file {
		t.Fatalf("first %+v", first)
	}
	q.Retry(first.ID)
	second := <-done
	if second.Status != Done || second.Path != file {
		t.Fatalf("second %+v", second)
	}
}

func waitFor(t *testing.T, q *Queue, status Status) Item {
	t.Helper()
	for range 1000 {
		if it := q.Items()[0]; it.Status == status {
			return it
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("never reached %s: %+v", status, q.Items())
	return Item{}
}
