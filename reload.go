package main

import (
	"os"
	"time"

	"github.com/egoist/mygo"
)

// Reload Window: GoRex quits and starts again with the same arguments, as
// ssh root@host, from the file it started from, which a rebuild has
// replaced (make dev moves a running one aside): the new build comes up
// on the same host, the layout restored and the sessions attached again,
// which the server kept. Closing asks about unsaved changes, as a quit
// does, and Cancel stops the reload.

// reloadWindow quits the window's GoRex and starts it again.
func (a *App) reloadWindow() {
	mygo.App.Relaunch()
}

// startExe is the file GoRex started from, as it was named then: a
// rebuild puts the new build there.
var startExe, _ = os.Executable()

// watchRebuild tells, in a development build, when the file GoRex started
// from changes, as a rebuild replaces it: a banner then offers to reload.
// Releases update themselves, and start again when they do.
func (a *App) watchRebuild() {
	if !mygo.IsDev() || startExe == "" {
		return
	}
	fi, err := os.Stat(startExe)
	if err != nil {
		return
	}
	start := fi.ModTime()
	go func() {
		for range time.Tick(3 * time.Second) {
			fi, err := os.Stat(startExe)
			if err != nil || fi.ModTime().Equal(start) {
				continue
			}
			a.update(func() {
				a.rebuilt = true
				a.changed()
			})
			return
		}
	}()
}
