// Package ytdlp runs yt-dlp as a child process and reads its progress.
package ytdlp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// extraPaths covers Homebrew locations absent from PATH when launched from
// Finder. Without ffmpeg and deno, yt-dlp still exits 0, leaving unmerged
// streams and reporting a path that does not exist.
var extraPaths = []string{"/opt/homebrew/bin", "/usr/local/bin"}

const (
	maxScanLine = 1 << 20
	maxStderr   = 64 << 10
)

// Update is what a running download reports.
type Update struct {
	Title    string
	Fraction float64
	Done     int64 // bytes, across all streams
	Total    int64 // bytes, 0 until known
	Speed    int64 // bytes per second, 0 until known
	Merging  bool
}

// Download fetches url into dir and returns the path of the finished file. By
// then it has written the video's preview to thumbnail, a .jpg path, if the
// site has one.
func Download(ctx context.Context, url, dir, thumbnail string, update func(Update)) (string, error) {
	ytdlpBin, err := findTool("yt-dlp")
	if err != nil {
		return "", err
	}
	ffmpegBin, err := findTool("ffmpeg")
	if err != nil {
		return "", err
	}
	denoBin, err := findTool("deno")
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "vidbar-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	cmd := exec.CommandContext(ctx, ytdlpBin, arguments(url, dir, tmp, thumbnail, ffmpegBin, denoBin)...)
	// ffmpeg is yt-dlp's child, so a cancel has to reach the whole group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	stderr := &tailBuffer{max: maxStderr}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	path := read(stdout, update)
	if err := cmd.Wait(); err != nil {
		return "", failure(stderr.String(), err)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("yt-dlp produced no file: %w", err)
	}
	return path, nil
}

func findTool(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range extraPaths {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s is missing: run brew install yt-dlp ffmpeg deno", name)
}

func arguments(url, dir, tmp, thumbnail, ffmpegBin, denoBin string) []string {
	// yt-dlp expands % in templates, and gives the image its site's format
	// before converting it.
	base := strings.ReplaceAll(strings.TrimSuffix(thumbnail, ".jpg"), "%", "%%")
	return []string{
		"--ignore-config",
		"--ffmpeg-location", ffmpegBin,
		"--js-runtimes", "deno:" + denoBin,
		"--no-playlist", "--playlist-items", "1",
		"-t", "mp4",
		"--no-simulate", "--newline", "--progress", "--progress-delta", "0.5",
		"--print", `before_dl:{"title":%(title)j,"formats":%(format_id)j,"size":%(filesize,filesize_approx|0)d}`,
		"--print", `after_move:{"path":%(filepath)j}`,
		"--progress-template", `download:{"format":%(info.format_id)j,"done":%(progress.downloaded_bytes|0)d,"total":%(progress.total_bytes,progress.total_bytes_estimate|0)d,"speed":%(progress.speed|0)d}`,
		"--progress-template", `postprocess:{"postprocess":%(progress.status)j}`,
		// Copied paths get pasted into terminals, which split on spaces and
		// glob on brackets.
		"--parse-metadata", "title:(?P<slug>.+)",
		"--replace-in-metadata", "slug", `[^\w.]+`, "-",
		"-P", "home:" + dir, "-P", "temp:" + tmp,
		"-o", "%(slug).80B-%(id)s.%(ext)s",
		"--write-thumbnail", "--convert-thumbnails", "jpg",
		"--ppa", "ThumbnailsConvertor:-vf scale=320:-2",
		"-o", "thumbnail:" + base + ".%(ext)s",
		"--", url,
	}
}

// read follows the JSON lines the templates in arguments print, and returns
// the path of the finished file.
func read(r io.Reader, update func(Update)) string {
	var current Update
	var s streams
	var path string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScanLine)
	for scanner.Scan() {
		var line struct {
			Title       string
			Formats     string
			Path        string
			Format      string
			Postprocess string
			Size        int64
			Done        int64
			Total       int64
			Speed       int64
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		switch {
		case line.Path != "":
			path = line.Path
		case line.Title != "":
			current.Title = line.Title
			s.count, s.size = strings.Count(line.Formats, "+")+1, line.Size
			update(current)
		case line.Postprocess != "":
			current.Merging = true
			current.Fraction = 1
			current.Speed = 0
			update(current)
		case line.Format != "":
			current.Merging = false
			current.Fraction, current.Done, current.Total = s.progress(line.Format, line.Done, line.Total)
			current.Speed = line.Speed
			update(current)
		}
	}
	return path
}

// tailBuffer keeps only the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n >= b.max {
		b.buf = append(b.buf[:0], p[n-b.max:]...)
		return n, nil
	}
	if over := len(b.buf) + n - b.max; over > 0 {
		copy(b.buf, b.buf[over:])
		b.buf = b.buf[:len(b.buf)-over]
	}
	b.buf = append(b.buf, p...)
	return n, nil
}

func (b *tailBuffer) String() string { return string(b.buf) }

// streams folds per-stream byte counts into one progress: a merged download
// is a video stream and then an audio stream, each counting from zero.
type streams struct {
	count    int
	size     int64 // yt-dlp's estimate of all streams together, or 0
	format   string
	finished int
	bytes    int64 // of the finished streams
	current  int64 // of the running stream
}

func (s *streams) progress(format string, done, total int64) (fraction float64, allDone, allTotal int64) {
	if s.format != "" && format != s.format {
		s.finished++
		s.bytes += s.current
	}
	s.format, s.current = format, done
	allDone = s.bytes + done
	allTotal = s.bytes + total
	// The last stream knows its own size better than the estimate does.
	if s.finished+1 < s.count {
		allTotal = max(allTotal, s.size)
	}
	if s.size > 0 && allTotal > 0 {
		return min(float64(allDone)/float64(allTotal), 1), allDone, allTotal
	}
	stream := 0.0
	if total > 0 {
		stream = min(float64(done)/float64(total), 1)
	}
	return (float64(s.finished) + stream) / float64(max(s.count, 1)), allDone, allTotal
}

// failure prefers yt-dlp's own last error to the bare exit status.
func failure(stderr string, err error) error {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if message, ok := strings.CutPrefix(lines[i], "ERROR: "); ok {
			return errors.New(message)
		}
	}
	return err
}
