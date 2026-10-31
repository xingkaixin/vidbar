package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/xingkaixin/mygo-native-ui/ui/swipeactions"

	"github.com/xingkaixin/vidbar/internal/downloads"
)

func icon(path string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="` + path + `"/></svg>`))
}

var (
	trashIcon = icon("M3.45 6.3h17.1M9.15 6.3V3.45h5.7v2.85M5.35 6.3l0.95 14.25h11.4l0.95-14.25M10.1 10.1v6.65m3.8-6.65v6.65")
	retryIcon = icon("M 3.963 4.856 v 5.358 h 5.358 m -5.358 0 a 8.037 8.037 0 0 1 14.288 -3.572 m 1.786 12.502 v -5.358 h -5.358 m 5.358 0 a 8.037 8.037 0 0 1 -14.288 3.572")
	closeIcon = icon("M6.3 6.3l11.4 11.4M17.7 6.3L6.3 17.7")
)

// secondary stands in for the theme's muted text, which the popover's
// material makes unreadable over dark windows.
func secondary(t *ui.Theme) ui.Color { return t.Text.Alpha(0.7) }

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	items := a.queue.Items()
	a.list.Key = func(row int) any { return items[row].ID }

	ui.Column(c).Fill().Children(func() {
		input := ui.TextInput(c.Key("url"), &a.url).AutoFocus().Label("视频链接").Placeholder("粘贴视频链接，按 Enter 下载").Margin(t.Space(3))
		if input.Submitted() {
			if url := strings.TrimSpace(a.url); url != "" {
				a.queue.Add(url)
			}
			a.url = ""
		}
		ui.Divider(c)
		ui.List(c, &a.list, len(items), func(i int) { a.row(c, items[i]) }).Grow(1).Label("下载记录").Children(func() {
			if len(items) == 0 {
				ui.Text(c, "暂无下载记录").TextColor(secondary(t)).Padding(t.Space(8)).AlignSelf(ui.Center)
			}
		})
		ui.Divider(c)
		ui.Row(c).Gap(t.Space(2)).Padding(t.Space(2), t.Space(3)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, shortPath(a.queue.Dir())).Grow(1).SingleLine().FontSize(t.FontSize - 1).TextColor(secondary(t))
			if ui.Button(c, "更改").Label("更改下载目录").Clicked() {
				a.chooseDir()
			}
		})
	})
}

func (a *app) row(c *ui.Context, it downloads.Item) {
	t := c.Theme()
	remove := swipeactions.Action{Label: "删除", Icon: trashIcon, Color: t.Danger}
	retry := swipeactions.Action{Label: "重试", Icon: retryIcon, Color: t.Accent}
	var actions []swipeactions.Action
	switch it.Status {
	case downloads.Queued, downloads.Running:
		actions = []swipeactions.Action{{Label: "取消", Icon: closeIcon, Color: t.Danger}}
	case downloads.Failed:
		actions = []swipeactions.Action{remove, retry}
	default:
		actions = []swipeactions.Action{remove}
	}

	ev := swipeactions.Row(c.Key(it.ID), &a.swipe, nil, actions, func() {
		ui.Column(c).Gap(t.Space(1)).Padding(t.Space(2), t.Space(3)).Children(func() {
			ui.Text(c, it.Title).SingleLine().FontWeight(500)
			if it.Status == downloads.Running {
				ui.Progress(c, it.Progress).Label("下载进度")
				return
			}
			detail := ui.Text(c, a.detail(it, time.Now())).FontSize(t.FontSize - 1).TextColor(secondary(t)).MaxLines(2)
			if it.Status == downloads.Failed {
				detail.TextColor(t.Danger)
			}
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
		c.Toast("已复制文件路径")
	}
}

func (a *app) detail(it downloads.Item, now time.Time) string {
	switch {
	case it.Status == downloads.Queued:
		return "等待下载"
	case it.Status == downloads.Failed:
		return it.Error
	case a.missing[it.ID]:
		return "文件已不存在"
	}
	return relativeTime(it.Time, now) + " · " + filepath.Base(it.Path)
}

// remove trashes the file first, so that a record is never lost while its
// file stays on disk.
func (a *app) remove(c *ui.Context, it downloads.Item) {
	if it.Status == downloads.Done && !a.missing[it.ID] {
		if err := mygo.Shell.TrashItem(it.Path); err != nil {
			c.Toast("删除失败: " + err.Error())
			return
		}
	}
	a.queue.Remove(it.ID)
}

func relativeTime(t, now time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	age := now.Sub(t)
	switch {
	case age < time.Minute:
		return "刚刚"
	case age < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(age.Minutes()))
	case t.After(today):
		return fmt.Sprintf("%d 小时前", int(age.Hours()))
	case t.After(today.AddDate(0, 0, -1)):
		return "昨天"
	}
	return t.Format("1月2日")
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
