package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	good     = Sample{HasRoute: true, Gateway: "192.168.1.1", GatewayUp: true, InternetUp: true, DNSUp: true}
	ispDown  = Sample{HasRoute: true, Gateway: "192.168.1.1", GatewayUp: true}
	rtrDown  = Sample{HasRoute: true, Gateway: "192.168.1.1"}
	noRoute  = Sample{}
	dnsDown  = Sample{HasRoute: true, Gateway: "192.168.1.1", GatewayUp: true, InternetUp: true}
	baseTime = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		s     Sample
		known bool
		want  Cause
	}{
		{"healthy", good, true, CauseNone},
		{"router ignores checks but internet works", Sample{HasRoute: true, Gateway: "10.0.0.1", InternetUp: true, DNSUp: true}, false, CauseNone},
		{"dns only", dnsDown, true, CauseDNS},
		{"no route", noRoute, false, CausePC},
		{"router up internet down", ispDown, true, CauseISP},
		{"known router gone", rtrDown, true, CauseRouter},
		{"router never answered", rtrDown, false, CauseUnclear},
	}
	for _, c := range cases {
		if got := Classify(c.s, c.known); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// run feeds samples spaced step apart, starting after the startup grace period.
type rig struct {
	t     *testing.T
	store *Store
	mon   *Monitor
	now   time.Time
}

func newRig(t *testing.T) *rig {
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, store: st, mon: NewMonitor(st), now: baseTime}
	r.feed(good, 5, 5*time.Second) // past the startup grace period
	return r
}

func (r *rig) feed(s Sample, n int, step time.Duration) Status {
	var st Status
	for i := 0; i < n; i++ {
		r.now = r.now.Add(step)
		s.Time = r.now
		st = r.mon.Step(s)
	}
	return st
}

func (r *rig) outages() []Outage { return r.store.Outages(baseTime.Add(-time.Hour)) }

func TestOneFailedCheckIsNotAnOutage(t *testing.T) {
	r := newRig(t)
	r.feed(ispDown, 1, 5*time.Second)
	r.feed(good, 5, 5*time.Second)
	if n := len(r.outages()); n != 0 {
		t.Fatalf("got %d outages, want 0", n)
	}
}

func TestOutageTimesAndCause(t *testing.T) {
	r := newRig(t)
	r.now = r.now.Add(5 * time.Second)
	start := r.now
	s := ispDown
	s.Time = r.now
	r.mon.Step(s)
	st := r.feed(ispDown, 4, 2*time.Second)
	if st.State != "outage" || st.Cause != CauseISP {
		t.Fatalf("status = %s/%s", st.State, st.Cause)
	}
	r.feed(good, 1, 2*time.Second)
	firstGood := r.now
	if o := r.outages()[0]; o.End != nil {
		t.Fatal("one good check shouldn't end an outage")
	}
	r.feed(good, 1, 2*time.Second)
	o := r.outages()[0]
	if o.End == nil || !o.End.Equal(firstGood) || !o.Start.Equal(start) {
		t.Fatalf("got %v to %v, want %v to %v", o.Start, o.End, start, firstGood)
	}
	if o.Cause != CauseISP {
		t.Fatalf("cause = %s", o.Cause)
	}
}

func TestBriefGoodCheckDoesNotSplitAnOutage(t *testing.T) {
	r := newRig(t)
	r.feed(ispDown, 3, 2*time.Second)
	r.feed(good, 1, 2*time.Second)
	r.feed(ispDown, 3, 2*time.Second)
	r.feed(good, 2, 2*time.Second)
	if n := len(r.outages()); n != 1 {
		t.Fatalf("got %d outages, want 1", n)
	}
}

func TestPrimaryCauseIsTheLongest(t *testing.T) {
	r := newRig(t)
	r.feed(rtrDown, 3, 2*time.Second) // router rebooting
	r.feed(ispDown, 30, 2*time.Second)
	r.feed(good, 2, 2*time.Second)
	if c := r.outages()[0].Cause; c != CauseISP {
		t.Fatalf("cause = %s, want isp", c)
	}
}

func TestRouterIsOnlyBlamedOnceItHasAnswered(t *testing.T) {
	st, _ := OpenStore(t.TempDir())
	r := &rig{t: t, store: st, mon: NewMonitor(st), now: baseTime}
	r.feed(Sample{HasRoute: true, Gateway: "192.168.1.1", InternetUp: true, DNSUp: true}, 5, 5*time.Second)
	r.feed(rtrDown, 3, 2*time.Second)
	r.feed(Sample{HasRoute: true, Gateway: "192.168.1.1", InternetUp: true, DNSUp: true}, 2, 2*time.Second)
	if c := r.outages()[0].Cause; c != CauseUnclear {
		t.Fatalf("cause = %s, want unclear", c)
	}
}

func TestSleepEndsOutageAndNetworkGetsTimeToReconnect(t *testing.T) {
	r := newRig(t)
	r.feed(ispDown, 3, 2*time.Second)
	lastCheck := r.now
	r.now = r.now.Add(8 * time.Hour) // asleep
	s := noRoute
	s.Time = r.now
	st := r.mon.Step(s)
	if st.State != "checking" {
		t.Fatalf("state after wake = %s", st.State)
	}
	o := r.outages()[0]
	if o.End == nil || !o.End.Equal(lastCheck) || o.Note == "" {
		t.Fatalf("outage wasn't closed at the last check: %+v", o)
	}
	r.feed(noRoute, 5, 2*time.Second) // still reconnecting, inside the grace period
	r.feed(good, 3, 2*time.Second)
	if n := len(r.outages()); n != 1 {
		t.Fatalf("waking up created an outage: %d", n)
	}
	if got := r.store.StatsFor(1, r.now).MonitoredSeconds; got > 3600 {
		t.Fatalf("sleep counted as monitored time: %v", got)
	}
}

func TestSleepDetectionIgnoresMonotonicClock(t *testing.T) {
	// Go's clock reading can hide sleep on Windows, so Step must strip it.
	r := newRig(t)
	a := time.Now()
	s := good
	s.Time = a
	r.mon.Step(s)
	if r.mon.last != a.Round(0) {
		t.Fatal("monotonic reading wasn't stripped")
	}
}

func TestStartupGrace(t *testing.T) {
	st, _ := OpenStore(t.TempDir())
	r := &rig{t: t, store: st, mon: NewMonitor(st), now: baseTime}
	r.feed(noRoute, 3, 2*time.Second)
	if n := len(r.outages()); n != 0 {
		t.Fatalf("got %d outages during startup", n)
	}
	r.feed(noRoute, 10, 2*time.Second)
	if n := len(r.outages()); n != 1 {
		t.Fatalf("a real outage after startup wasn't logged: %d", n)
	}
}

func TestCrashRecoveryClosesOpenOutage(t *testing.T) {
	dir := t.TempDir()
	st, _ := OpenStore(dir)
	o := &Outage{Start: baseTime, LastSeen: baseTime.Add(time.Minute), Cause: CauseISP, CauseSeconds: map[Cause]float64{CauseISP: 60}}
	st.AddOutage(o)
	st2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := st2.Outages(baseTime.Add(-time.Hour))[0]
	if got.End == nil || !got.End.Equal(baseTime.Add(time.Minute)) || got.Note != noteInterrupted {
		t.Fatalf("got %+v", got)
	}
}

func TestDamagedHistoryIsKeptAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(path + ".damaged-*")
	if len(m) != 1 {
		t.Fatalf("damaged copy not kept: %v", m)
	}
}

func TestStatsClipToPeriodAndMonitoredTime(t *testing.T) {
	st, _ := OpenStore(t.TempDir())
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local)
	end := now.Add(-7*24*time.Hour + time.Hour)
	st.AddOutage(&Outage{Start: end.Add(-3 * time.Hour), End: &end, Cause: CauseISP})
	st.AddMonitored(now.Add(-time.Hour), 10*time.Hour)
	s := st.StatsFor(7, now)
	if s.Count != 1 || s.DowntimeSeconds != 3600 {
		t.Fatalf("got %+v, want 1 outage clipped to 1h", s)
	}
	if s.UptimePercent == nil || *s.UptimePercent < 89.9 || *s.UptimePercent > 90.1 {
		t.Fatalf("uptime = %v", s.UptimePercent)
	}
}

func TestProviderReportFiltersOutages(t *testing.T) {
	st, _ := OpenStore(t.TempDir())
	now := time.Now()
	e1 := now.Add(-time.Hour)
	st.AddOutage(&Outage{Start: e1.Add(-5 * time.Minute), End: &e1, Cause: CauseISP})
	st.AddOutage(&Outage{Start: e1.Add(-time.Minute), End: &e1, Cause: CausePC})
	d := BuildReport(st, ReportOptions{Name: "A (B) \\ C", Days: 7, ProviderOnly: true}, now)
	if d.Count != 1 || d.Other != 1 || d.Rows[0].Duration != "5m 0s" {
		t.Fatalf("got %+v", d)
	}
	pdf := RenderPDF(d)
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.HasSuffix(bytes.TrimSpace(pdf), []byte("%%EOF")) {
		t.Fatal("not a complete PDF")
	}
	if !bytes.Contains(pdf, []byte(`A \(B\) \\ C`)) {
		t.Fatal("special characters weren't escaped")
	}
}

func TestReportPaginatesLongHistory(t *testing.T) {
	st, _ := OpenStore(t.TempDir())
	now := time.Now()
	for i := 0; i < 200; i++ {
		end := now.Add(-time.Duration(i+1) * time.Hour)
		st.AddOutage(&Outage{Start: end.Add(-time.Minute), End: &end, Cause: CauseISP})
	}
	pdf := RenderPDF(BuildReport(st, ReportOptions{Days: 30, ProviderOnly: true}, now))
	if n := bytes.Count(pdf, []byte("/Type /Page ")); n < 3 {
		t.Fatalf("got %d pages", n)
	}
}

func TestCSVExport(t *testing.T) {
	st, _ := OpenStore(t.TempDir())
	now := time.Now()
	e := now.Add(-time.Hour)
	st.AddOutage(&Outage{Start: e.Add(-time.Minute), End: &e, Cause: CauseISP})
	var b bytes.Buffer
	if err := WriteCSV(&b, st, 7, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Internet provider or modem") {
		t.Fatal(b.String())
	}
}

type fakeNet struct {
	mu                           sync.Mutex
	route, gwPing, pub, tls, dns bool
	pings, tlsCalls, dnsCalls    int
}

func (f *fakeNet) Route() (string, bool) { return "192.168.1.1", f.route }
func (f *fakeNet) Ping(ip string, _ time.Duration) (bool, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pings++
	if ip == "192.168.1.1" {
		return f.gwPing, 1
	}
	return f.pub, 20
}
func (f *fakeNet) LinkDown() bool                          { return false }
func (f *fakeNet) GatewayTCP(context.Context, string) bool { return false }
func (f *fakeNet) PublicTLS(context.Context) (bool, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tlsCalls++
	return f.tls, 40
}
func (f *fakeNet) DNS(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dnsCalls++
	return f.dns
}

func TestHealthyChecksStayLight(t *testing.T) {
	f := &fakeNet{route: true, gwPing: true, pub: true, tls: true, dns: true}
	c := NewChecker(f)
	now := baseTime
	var s Sample
	for i := 0; i < 29; i++ {
		s = c.Check(context.Background(), now)
		now = now.Add(time.Second)
	}
	if !s.InternetUp || !s.DNSUp || !s.GatewayUp {
		t.Fatalf("got %+v", s)
	}
	if f.tlsCalls != 0 || f.dnsCalls != 1 {
		t.Fatalf("tls=%d dns=%d, want only the first lookup in 30 seconds", f.tlsCalls, f.dnsCalls)
	}
	c.Check(context.Background(), now.Add(time.Second))
	if f.dnsCalls != 2 {
		t.Fatalf("periodic lookup didn't run: %d", f.dnsCalls)
	}
}

func TestFailedPingTriggersFullCheck(t *testing.T) {
	f := &fakeNet{route: true, gwPing: true, pub: true, tls: true, dns: true}
	c := NewChecker(f)
	c.Check(context.Background(), baseTime)
	f.mu.Lock()
	f.pub, f.tls = false, false
	f.mu.Unlock()
	s := c.Check(context.Background(), baseTime.Add(time.Second))
	if s.InternetUp || f.tlsCalls != 1 {
		t.Fatalf("got %+v, tls=%d", s, f.tlsCalls)
	}
}

func TestDNSFailureIsCheckedAgainImmediately(t *testing.T) {
	f := &fakeNet{route: true, gwPing: true, pub: true, dns: false}
	c := NewChecker(f)
	c.Check(context.Background(), baseTime)
	calls := f.dnsCalls
	f.mu.Lock()
	f.dns = true
	f.mu.Unlock()
	s := c.Check(context.Background(), baseTime.Add(time.Second))
	if !s.DNSUp || f.dnsCalls != calls+1 {
		t.Fatalf("got %+v, calls=%d", s, f.dnsCalls)
	}
}

func TestNetworksThatBlockPingUseOccasionalTLS(t *testing.T) {
	f := &fakeNet{route: true, gwPing: true, pub: false, tls: true, dns: true}
	c := NewChecker(f)
	now := baseTime
	for i := 0; i < 20; i++ {
		if s := c.Check(context.Background(), now); !s.InternetUp {
			t.Fatalf("check %d: internet reported down", i)
		}
		now = now.Add(time.Second)
	}
	if f.tlsCalls > 6 {
		t.Fatalf("TLS ran %d times in 20 seconds", f.tlsCalls)
	}
}

func TestSlowReadingDoesNotFlattenGraph(t *testing.T) {
	var pts []CheckPoint
	for i := 0; i < 100; i++ {
		pts = append(pts, CheckPoint{T: baseTime, Ms: 17, OK: true})
	}
	pts = append(pts, CheckPoint{T: baseTime, Ms: 1500, OK: true})
	if s := GraphScale(pts, baseTime); s != 50 {
		t.Fatalf("scale = %d, want 50", s)
	}
}

func TestTLSTimeIsNotGraphed(t *testing.T) {
	f := &fakeNet{route: true, gwPing: true, pub: false, tls: true, dns: true}
	if s := NewChecker(f).Check(context.Background(), baseTime); !s.InternetUp || s.LatencyMs != -1 {
		t.Fatalf("got %+v", s)
	}
}

func TestNoRoute(t *testing.T) {
	f := &fakeNet{}
	if s := NewChecker(f).Check(context.Background(), baseTime); s.HasRoute || s.InternetUp {
		t.Fatalf("got %+v", s)
	}
}

func TestEmptyReportSaysSo(t *testing.T) {
	if !bytes.Contains(RenderPDF(ReportData{}), []byte("No outages were logged")) {
		t.Fatal("missing empty message")
	}
}

func TestMigrateOldData(t *testing.T) {
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "Dropline"), filepath.Join(base, "DropTrace")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "history.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateOldData(oldDir, newDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(newDir, "history.json")); err != nil {
		t.Fatal("history wasn't moved")
	}
	// A later old folder must never replace existing DropTrace history.
	_ = os.MkdirAll(oldDir, 0o755)
	_ = os.WriteFile(filepath.Join(oldDir, "history.json"), []byte("old"), 0o600)
	if err := migrateOldData(oldDir, newDir); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(newDir, "history.json")); string(raw) != "{}" {
		t.Fatal("existing history was overwritten")
	}
}

func TestPingAnswerInvalid(t *testing.T) {
	wait := 900 * time.Millisecond
	if !pingAnswerInvalid(icmpTimedOut, 5*time.Millisecond, wait) {
		t.Fatal("instant timeout should be invalid")
	}
	if pingAnswerInvalid(icmpTimedOut, wait, wait) {
		t.Fatal("real timeout should count")
	}
	if pingAnswerInvalid(11003, time.Millisecond, wait) {
		t.Fatal("other errors should count")
	}
}

func TestShortUnplugBlamesThisPC(t *testing.T) {
	st, _ := OpenStore(t.TempDir())
	m := NewMonitor(st)
	now := time.Now()
	step := func(s Sample) {
		s.Time = now
		s.GatewayMs, s.LatencyMs = -1, -1
		m.Step(s)
		now = now.Add(2 * time.Second)
	}
	good := Sample{HasRoute: true, Gateway: "192.168.1.1", GatewayUp: true, InternetUp: true, DNSUp: true}
	m.graceUntil = now
	for i := 0; i < 3; i++ {
		step(good)
	}
	m.graceUntil = now
	// The cable is out briefly, then the router takes longer than that to answer.
	for i := 0; i < 3; i++ {
		step(Sample{LinkDown: true})
	}
	for i := 0; i < 15; i++ {
		step(Sample{HasRoute: true, Gateway: "192.168.1.1"})
	}
	for i := 0; i < 5; i++ {
		step(good)
	}
	all := st.Outages(now.Add(-time.Hour))
	if len(all) != 1 || all[0].End == nil {
		t.Fatalf("outages: %+v", all)
	}
	if all[0].Cause != CausePC {
		t.Fatalf("cause %s, seconds %v", all[0].Cause, all[0].CauseSeconds)
	}
}

func TestRouterOutageWithoutLinkLossStaysRouter(t *testing.T) {
	o := &Outage{Cause: CausePC, CauseSeconds: map[Cause]float64{CausePC: 3, CauseRouter: 30}}
	if primaryCause(o) != CauseRouter {
		t.Fatal(primaryCause(o))
	}
	o.LinkLost = true
	if primaryCause(o) != CausePC {
		t.Fatal(primaryCause(o))
	}
}

func TestCauseInSentenceKeepsNames(t *testing.T) {
	if got := causeInSentence(CausePC); !strings.Contains(got, "PC") || !strings.HasPrefix(got, "this") {
		t.Fatal(got)
	}
	if got := causeInSentence(CauseISP); got[0] < 'a' || got[0] > 'z' {
		t.Fatal(got)
	}
}
