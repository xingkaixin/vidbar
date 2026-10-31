// Package ytdlp runs yt-dlp as a child process and reads its progress.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// homebrew is spelled out because apps opened from Finder do not have it on
// PATH. Without ffmpeg and deno, yt-dlp still exits 0, leaving unmerged
// streams and reporting a path that does not exist.
const homebrew = "/opt/homebrew/bin/"

// Update is what a running download reports.
type Update struct {
	Title    string
	Fraction float64
}

// Download fetches url into dir and returns the path of the finished file.
func Download(ctx context.Context, url, dir string, update func(Update)) (string, error) {
	for _, tool := range []string{"yt-dlp", "ffmpeg", "deno"} {
		if _, err := os.Stat(homebrew + tool); err != nil {
			return "", fmt.Errorf("缺少 %s，请运行 brew install yt-dlp ffmpeg", tool)
		}
	}
	tmp, err := os.MkdirTemp("", "vidbar-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	cmd := exec.CommandContext(ctx, homebrew+"yt-dlp", arguments(url, dir, tmp)...)
	// ffmpeg is yt-dlp's child, so a cancel has to reach the whole group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
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
		return "", fmt.Errorf("yt-dlp 没有生成文件: %w", err)
	}
	return path, nil
}

func arguments(url, dir, tmp string) []string {
	return []string{
		"--ignore-config",
		"--ffmpeg-location", homebrew + "ffmpeg",
		"--js-runtimes", "deno:" + homebrew + "deno",
		"--no-playlist", "--playlist-items", "1",
		"-t", "mp4",
		"--no-simulate", "--newline", "--progress", "--progress-delta", "0.5",
		"--print", `before_dl:{"title":%(title)j,"formats":%(format_id)j}`,
		"--print", `after_move:{"path":%(filepath)j}`,
		"--progress-template", `download:{"format":%(info.format_id)j,"done":%(progress.downloaded_bytes|0)d,"total":%(progress.total_bytes,progress.total_bytes_estimate|0)d}`,
		// Copied paths get pasted into terminals, which split on spaces and
		// glob on brackets.
		"--parse-metadata", "title:(?P<slug>.+)",
		"--replace-in-metadata", "slug", `[^\w.]+`, "-",
		"-P", "home:" + dir, "-P", "temp:" + tmp,
		"-o", "%(slug).80B-%(id)s.%(ext)s",
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
	for scanner.Scan() {
		var line struct {
			Title   string
			Formats string
			Path    string
			Format  string
			Done    int64
			Total   int64
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		switch {
		case line.Path != "":
			path = line.Path
		case line.Title != "":
			current.Title = line.Title
			s.count = strings.Count(line.Formats, "+") + 1
			update(current)
		case line.Format != "":
			current.Fraction = s.fraction(line.Format, line.Done, line.Total)
			update(current)
		}
	}
	return path
}

// streams folds per-stream byte counts into one fraction: a merged download
// is a video stream and then an audio stream, each counting from zero.
type streams struct {
	count    int
	format   string
	finished int
}

func (s *streams) fraction(format string, done, total int64) float64 {
	if s.format != "" && format != s.format {
		s.finished++
	}
	s.format = format
	stream := 0.0
	if total > 0 {
		stream = min(float64(done)/float64(total), 1)
	}
	return (float64(s.finished) + stream) / float64(max(s.count, 1))
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
