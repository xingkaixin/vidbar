// Vidbar is a menu bar downloader for videos, on top of yt-dlp.
package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/xingkaixin/mygo-native-ui/ui/swipeactions"

	"github.com/xingkaixin/vidbar/internal/downloads"
)

//go:embed resources/tray.png
var trayIcon []byte

// app is read and changed on the main thread. Downloads outlive the panel,
// so they live in the queue and the panel only shows them.
type app struct {
	queue   *downloads.Queue
	tray    *mygo.Tray
	panel   *mygo.Window // the open panel, or nil
	url     string
	list    ui.ListState
	swipe   swipeactions.State
	missing map[string]bool // done downloads whose files are gone, by ID
}

// logCrashes keeps panics, which no terminal shows for an app opened from
// Finder.
func logCrashes() {
	dir, err := mygo.App.Path(mygo.PathLogs)
	if err == nil {
		err = os.MkdirAll(dir, 0o755)
	}
	if err != nil {
		log.Printf("crash log: %v", err)
		return
	}
	file, err := os.OpenFile(filepath.Join(dir, "crash.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("crash log: %v", err)
		return
	}
	defer file.Close()
	if err := debug.SetCrashOutput(file, debug.CrashOptions{}); err != nil {
		log.Printf("crash log: %v", err)
	}
}

func (a *app) ready() {
	logCrashes()
	data, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	dir, err := mygo.App.Path(mygo.PathDownloads)
	if err != nil {
		log.Fatal(err)
	}
	// RunOnMain waits for the main thread, which Close blocks while the
	// worker that calls this winds down.
	a.queue = downloads.Open(filepath.Join(data, "history.json"), dir, func() { go mygo.RunOnMain(a.changed) })

	a.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon, IconIsTemplate: true, ToolTip: "Vidbar"})
	if err != nil {
		log.Fatal(err)
	}
	a.tray.OnClick(a.toggle)
	menu := mygo.NewMenu([]*mygo.MenuItem{{Label: "退出 Vidbar", Role: mygo.RoleQuit}})
	a.tray.OnRightClick(func() {
		if a.panel != nil {
			a.panel.Close()
		}
		// Set for this popup only: a tray's menu takes over its left click.
		a.tray.SetMenu(menu)
		defer a.tray.SetMenu(nil)
		a.tray.PopUpMenu()
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		a.queue.Close()
		a.tray.Destroy()
	})
}

// toggle opens the panel next to the icon, or closes it. The user cannot be
// in Finder while the panel is open, so checking files as it opens is enough.
func (a *app) toggle() {
	if a.panel != nil {
		a.panel.Close()
		return
	}
	a.missing = map[string]bool{}
	for _, it := range a.queue.Items() {
		if _, err := os.Stat(it.Path); it.Status == downloads.Done && err != nil {
			a.missing[it.ID] = true
		}
	}
	a.tray.SetTitle("")
	a.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      a.tray,
		Placement: mygo.PlacementBottom,
		Width:     360,
		Height:    460,
		Focusable: true,
		Shadow:    true,
		Popover:   true,
		Content:   ui.View(a.view),
	})
	a.panel.OnClosed(func() {
		a.panel = nil
		a.changed()
	})
}

// changed follows the queue. The icon's title is the only sign of a download
// while the panel is closed; while it is open the title stays off, because
// the popover does not follow the icon as a title changes the icon's width.
func (a *app) changed() {
	if a.panel != nil {
		a.panel.Invalidate()
		return
	}
	title := ""
	for _, it := range a.queue.Items() {
		if it.Status == downloads.Running {
			title = fmt.Sprintf("%d%%", int(it.Progress*100))
		}
	}
	a.tray.SetTitle(title)
}

func (a *app) chooseDir() {
	// Dialog.Open waits for the user, which must not hold the main thread.
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "选择下载目录", Directory: true, DefaultPath: a.queue.Dir()})
		if err != nil {
			log.Printf("choose directory: %v", err)
		}
		if len(paths) > 0 {
			a.queue.SetDir(paths[0])
		}
	}()
}

func main() {
	a := &app{}
	mygo.App.SetName("Vidbar")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	// The panel is the only window: keep running as it closes.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
