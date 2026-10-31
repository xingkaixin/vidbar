// Package downloads keeps the download history and runs one download at a
// time.
package downloads

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/xingkaixin/vidbar/internal/ytdlp"
)

// Status is where a download is.
type Status string

const (
	Queued  Status = "queued"
	Running Status = "running"
	Done    Status = "done"
	Failed  Status = "failed"
)

// Item is a download. Time is when it was added, then when it finished.
type Item struct {
	ID       string    `json:"id"`
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Path     string    `json:"path,omitempty"`
	Size     int64     `json:"size,omitempty"`
	Status   Status    `json:"status"`
	Error    string    `json:"error,omitempty"`
	Time     time.Time `json:"time"`
	Progress float64   `json:"-"`
}

// fetchFunc is the shape of ytdlp.Download.
type fetchFunc func(ctx context.Context, url, dir string, update func(ytdlp.Update)) (string, error)

type saved struct {
	Dir   string `json:"dir"`
	Items []Item `json:"items"`
}

// Queue is the history, newest first, and the worker that drains it.
type Queue struct {
	path     string
	fetch    fetchFunc
	onChange func()

	mu       sync.Mutex
	state    saved
	draining bool
	closed   bool
	cancel   context.CancelFunc
	worker   sync.WaitGroup
}

// Open loads the history at path. onChange runs on the worker's goroutine,
// or the caller's, after each change.
func Open(path, defaultDir string, onChange func()) *Queue {
	q := &Queue{path: path, fetch: ytdlp.Download, onChange: onChange, state: saved{Dir: defaultDir}}
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &q.state)
	}
	if err != nil && !os.IsNotExist(err) {
		log.Printf("read history: %v", err)
	}
	for i := range q.state.Items {
		if it := &q.state.Items[i]; it.Status == Queued || it.Status == Running {
			it.Status, it.Error = Failed, "下载被中断"
		}
	}
	return q
}

// Items returns the history.
func (q *Queue) Items() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Clone(q.state.Items)
}

// Dir returns the directory downloads go to.
func (q *Queue) Dir() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.state.Dir
}

// SetDir changes the directory of the downloads that start from now on.
func (q *Queue) SetDir(dir string) {
	q.change(func() { q.state.Dir = dir })
}

// Add queues a download of url.
func (q *Queue) Add(url string) {
	q.change(func() {
		id := strconv.FormatInt(time.Now().UnixNano(), 36)
		q.state.Items = slices.Insert(q.state.Items, 0, Item{ID: id, URL: url, Title: url, Status: Queued, Time: time.Now()})
		q.startWorker()
	})
}

// Retry queues a failed download again, as the newest.
func (q *Queue) Retry(id string) {
	q.change(func() {
		i := q.index(id)
		if i < 0 {
			return
		}
		it := q.state.Items[i]
		it.Status, it.Error, it.Time = Queued, "", time.Now()
		q.state.Items = slices.Insert(slices.Delete(q.state.Items, i, i+1), 0, it)
		q.startWorker()
	})
}

// Remove forgets a download, and stops it if it is running.
func (q *Queue) Remove(id string) {
	q.change(func() {
		i := q.index(id)
		if i < 0 {
			return
		}
		if q.state.Items[i].Status == Running {
			q.cancel()
		}
		q.state.Items = slices.Delete(q.state.Items, i, i+1)
	})
}

// Close stops the worker and waits for its download to die. What was running
// stays saved as running, which Open reports as interrupted.
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	if q.cancel != nil {
		q.cancel()
	}
	q.mu.Unlock()
	q.worker.Wait()
}

// change applies a change that lasts, under the lock.
func (q *Queue) change(fn func()) {
	q.mu.Lock()
	fn()
	q.save()
	q.mu.Unlock()
	q.onChange()
}

func (q *Queue) index(id string) int {
	return slices.IndexFunc(q.state.Items, func(it Item) bool { return it.ID == id })
}

func (q *Queue) startWorker() {
	if !q.draining {
		q.draining = true
		q.worker.Add(1)
		go q.drain()
	}
}

func (q *Queue) drain() {
	defer q.worker.Done()
	for {
		ctx, it, dir, ok := q.next()
		if !ok {
			return
		}
		path, err := q.fetch(ctx, it.URL, dir, func(u ytdlp.Update) { q.report(it.ID, u) })
		q.finish(it.ID, path, err)
	}
}

// next starts the oldest queued download, or retires the worker.
func (q *Queue) next() (context.Context, Item, string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, it := range slices.Backward(q.state.Items) {
		if it.Status == Queued && !q.closed {
			var ctx context.Context
			ctx, q.cancel = context.WithCancel(context.Background())
			q.state.Items[i].Status = Running
			q.state.Items[i].Progress = 0
			return ctx, q.state.Items[i], q.state.Dir, true
		}
	}
	q.draining = false
	return nil, Item{}, "", false
}

// report does not save: progress dies with the process.
func (q *Queue) report(id string, u ytdlp.Update) {
	q.mu.Lock()
	if i := q.index(id); i >= 0 {
		if u.Title != "" {
			q.state.Items[i].Title = u.Title
		}
		q.state.Items[i].Progress = u.Fraction
	}
	q.mu.Unlock()
	q.onChange()
}

func (q *Queue) finish(id, path string, err error) {
	q.change(func() {
		q.cancel()
		i := q.index(id)
		if i < 0 || q.closed {
			return
		}
		it := &q.state.Items[i]
		it.Time = time.Now()
		if err != nil {
			it.Status, it.Error = Failed, err.Error()
			return
		}
		it.Status, it.Path = Done, path
		if info, err := os.Stat(path); err == nil {
			it.Size = info.Size()
		}
	})
}

// save keeps the app running without history: the downloads still work.
func (q *Queue) save() {
	if err := q.write(); err != nil {
		log.Printf("save history: %v", err)
	}
}

// write replaces the file through a rename, so that a crash cannot leave half
// of it.
func (q *Queue) write() error {
	data, err := json.MarshalIndent(q.state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}
