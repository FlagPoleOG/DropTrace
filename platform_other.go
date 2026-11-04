//go:build !windows

package main

// Lets the logic and tests compile on other systems. DropTrace only ships for Windows.

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"
)

func dataDir() string {
	if d := os.Getenv("DROPTRACE_DATA"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "droptrace-dev")
}

func acquireSingleInstance() bool { return true }
func showExistingInstance()       {}
func fatal(msg string)            { log.Fatal(msg) }
func refreshAutostartPath()       {}
func startedBySignIn() bool       { return false }
func oldDataDir() string          { return "" }
func olderVersionRunning() bool   { return false }

type noNet struct{}

func newNetOps() NetOps                               { return noNet{} }
func (noNet) Route() (string, bool)                   { return "", false }
func (noNet) Ping(string, time.Duration) (bool, int)  { return false, 0 }
func (noNet) LinkDown() bool                          { return false }
func (noNet) GatewayTCP(context.Context, string) bool { return false }
func (noNet) PublicTLS(context.Context) (bool, int)   { return false, 0 }
func (noNet) DNS(context.Context) bool                { return false }

type noUI struct{}

func newPlatformUI(*Store, *Monitor, bool) PlatformUI { return noUI{} }
func (noUI) Run()                                     { select {} }
func (noUI) Update(Status, []CheckPoint)              {}
func (noUI) WindowVisible() bool                      { return false }
func (noUI) Notify(string, string)                    {}
