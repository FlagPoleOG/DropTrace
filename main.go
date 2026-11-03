// DropTrace logs internet dropouts and shows where each one most likely happened.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// Version and DonateURL can be set when building, for example:
// go build -ldflags "-X main.DonateURL=https://ko-fi.com/yourname"
var (
	Version   = "0.3.1"
	DonateURL = ""
)

// PlatformUI is the app window and tray icon.
type PlatformUI interface {
	Run()
	Update(st Status, points []CheckPoint)
	WindowVisible() bool
	Notify(title, body string)
}

func main() {
	debug.SetGCPercent(50) // keeps memory low for an app that runs all day
	background := false
	for _, a := range os.Args[1:] {
		if a == "--background" {
			background = true
		}
	}
	if startedBySignIn() {
		background = true
	}
	if olderVersionRunning() {
		fatal("An older version called Dropline is still running. Right-click its tray icon, choose Exit, and then open DropTrace again.")
	}
	dir := dataDir()
	if err := migrateOldData(oldDataDir(), dir); err != nil {
		log.Printf("couldn't move old history: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal("DropTrace can't create its data folder: " + err.Error())
	}
	if !acquireSingleInstance() {
		showExistingInstance()
		return
	}
	if f, err := os.OpenFile(filepath.Join(dir, "droptrace.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	store, err := OpenStore(dir)
	if err != nil {
		fatal("DropTrace can't open its history: " + err.Error())
	}
	refreshAutostartPath()

	mon := NewMonitor(store)
	ui := newPlatformUI(store, mon, !background)
	mon.onClose = func(o Outage) {
		if store.NotifyEnabled() {
			ui.Notify("Your connection is back",
				fmt.Sprintf("Down for %s. Likely cause: %s.", formatDuration(o.Duration(time.Now())), strings.ToLower(CauseInfoFor(o.Cause).Label)))
		}
	}
	go runChecks(mon, NewChecker(newNetOps()), ui)
	ui.Run() // blocks until the person chooses Exit
	if err := store.Save(); err != nil {
		log.Printf("final save failed: %v", err)
	}
}

func runChecks(mon *Monitor, c *Checker, ui PlatformUI) {
	var points []CheckPoint
	for {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		s := c.Check(ctx, start)
		cancel()
		s.Time = start
		st := mon.Step(s)
		points = mon.Recent(points)
		ui.Update(st, points)

		wait := TrayInterval
		if ui.WindowVisible() || st.State != "online" {
			wait = VisibleInterval
		}
		if rest := wait - time.Since(start); rest > 0 {
			time.Sleep(rest)
		}
	}
}

func formatDuration(d time.Duration) string {
	secs := int(d.Round(time.Second).Seconds())
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm %ds", secs/60, secs%60)
	case secs < 86400:
		return fmt.Sprintf("%dh %dm", secs/3600, secs%3600/60)
	}
	return fmt.Sprintf("%dd %dh", secs/86400, secs%86400/3600)
}
