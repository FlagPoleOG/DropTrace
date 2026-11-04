//go:build windows && droptrace_sim

package main

// Test build only: network results come from sim.json so each kind of outage can
// be shown on demand. The real app doesn't include this file.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type simNet struct{}

func newNetOps() NetOps { return simNet{} }

type simState struct {
	Route, Gateway, Internet, DNS bool
	LinkDown                      bool
	Ms                            int
}

func readSim() simState {
	s := simState{Route: true, Gateway: true, Internet: true, DNS: true, Ms: 24}
	if raw, err := os.ReadFile(filepath.Join(dataDir(), "sim.json")); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

func (simNet) Route() (string, bool) { return "192.168.1.1", readSim().Route }
func (simNet) Ping(ip string, _ time.Duration) (bool, int) {
	s := readSim()
	if ip == "192.168.1.1" {
		return s.Gateway, 1
	}
	return s.Internet, s.Ms + int(time.Now().UnixNano()/1e6%17)
}
func (simNet) LinkDown() bool                          { return readSim().LinkDown }
func (simNet) GatewayTCP(context.Context, string) bool { return false }
func (simNet) PublicTLS(context.Context) (bool, int)   { return false, -1 }
func (simNet) DNS(context.Context) bool                { return readSim().DNS }
