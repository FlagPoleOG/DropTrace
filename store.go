package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

// Outage is one logged dropout. End is nil while it's still happening.
type Outage struct {
	ID           int64             `json:"id"`
	Start        time.Time         `json:"start"`
	End          *time.Time        `json:"end,omitempty"`
	LastSeen     time.Time         `json:"lastSeen"`
	Cause        Cause             `json:"cause"`
	CauseSeconds map[Cause]float64 `json:"causeSeconds,omitempty"`
	Gateway      string            `json:"gateway,omitempty"`
	Note         string            `json:"note,omitempty"`
}

// Duration returns the outage length, measured to now if it's still open.
func (o Outage) Duration(now time.Time) time.Duration {
	end := now
	if o.End != nil {
		end = *o.End
	}
	if d := end.Sub(o.Start); d > 0 {
		return d
	}
	return 0
}

type stateFile struct {
	Version       int                `json:"version"`
	NextID        int64              `json:"nextId"`
	Outages       []*Outage          `json:"outages"`
	Days          map[string]float64 `json:"monitoredSecondsByDay"`
	KnownGateways []string           `json:"knownGateways"`
	NotifyOff     bool               `json:"notifyOff"`
}

// Store keeps history in one JSON file on this PC.
type Store struct {
	mu       sync.Mutex
	path     string
	data     stateFile
	lastSave time.Time
}

// OpenStore loads history, closing any outage left open by a crash or shutdown.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "history.json")}
	s.data = stateFile{Version: 1, NextID: 1, Days: map[string]float64{}}
	raw, err := os.ReadFile(s.path)
	if err == nil {
		var loaded stateFile
		if jerr := json.Unmarshal(raw, &loaded); jerr != nil {
			// Keep the damaged file instead of overwriting someone's history.
			_ = os.Rename(s.path, fmt.Sprintf("%s.damaged-%d", s.path, time.Now().Unix()))
		} else {
			s.data = loaded
			if s.data.Days == nil {
				s.data.Days = map[string]float64{}
			}
			if s.data.NextID < 1 {
				s.data.NextID = 1
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, o := range s.data.Outages {
		if o.End == nil {
			end := o.LastSeen
			o.End = &end
			o.Note = noteInterrupted
			o.Cause = primaryCause(o)
		}
	}
	return s, s.Save()
}

func primaryCause(o *Outage) Cause {
	best, bestSecs := o.Cause, -1.0
	// Fixed order keeps ties from changing between runs.
	for _, c := range []Cause{o.Cause, CauseISP, CauseRouter, CausePC, CauseDNS, CauseUnclear} {
		if secs, ok := o.CauseSeconds[c]; ok && secs > bestSecs {
			best, bestSecs = c, secs
		}
	}
	return best
}

// Save writes history atomically so a crash can't leave half a file.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", " ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.lastSave = time.Now()
	return nil
}

func (s *Store) saveSoonLocked(maxAge time.Duration) {
	if time.Since(s.lastSave) >= maxAge {
		_ = s.saveLocked()
	}
}

// RememberGateway records that this router has answered at least once.
func (s *Store) RememberGateway(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.data.KnownGateways {
		if g == ip {
			return
		}
	}
	s.data.KnownGateways = append(s.data.KnownGateways, ip)
	_ = s.saveLocked()
}

// GatewayKnown reports whether this router has ever answered.
func (s *Store) GatewayKnown(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.data.KnownGateways {
		if g == ip {
			return true
		}
	}
	return false
}

// AddMonitored adds time the app was really watching the connection.
func (s *Store) AddMonitored(at time.Time, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Days[at.Local().Format("2006-01-02")] += d.Seconds()
	s.saveSoonLocked(time.Minute)
}

// AddOutage stores a newly opened outage.
func (s *Store) AddOutage(o *Outage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o.ID = s.data.NextID
	s.data.NextID++
	s.data.Outages = append(s.data.Outages, o)
	_ = s.saveLocked()
}

// AddCauseTime adds time to one cause of an open outage.
func (s *Store) AddCauseTime(o *Outage, c Cause, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o.CauseSeconds[c] += d.Seconds()
}

// TouchOutage updates when an open outage was last seen.
func (s *Store) TouchOutage(o *Outage, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o.LastSeen = at
	s.saveSoonLocked(15 * time.Second)
}

// CloseOutage ends an outage and sets its final cause.
func (s *Store) CloseOutage(o *Outage, end time.Time, note string) Outage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if end.Before(o.Start) {
		end = o.Start
	}
	o.End, o.Note, o.Cause = &end, note, primaryCause(o)
	_ = s.saveLocked()
	return *o
}

// Snapshot returns a safe copy of an outage.
func (s *Store) Snapshot(o *Outage) Outage {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *o
	c.Cause = primaryCause(o)
	c.CauseSeconds = nil
	return c
}

// Outages returns outages overlapping [from, now], newest first.
func (s *Store) Outages(from time.Time) []Outage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Outage{}
	for _, o := range s.data.Outages {
		if o.End == nil || !o.End.Before(from) {
			c := *o
			c.CauseSeconds = nil
			if o.End == nil {
				c.Cause = primaryCause(o)
			}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	return out
}

// Stats summarizes a period.
type Stats struct {
	Days             int           `json:"days"`
	MonitoredSeconds float64       `json:"monitoredSeconds"`
	DowntimeSeconds  float64       `json:"downtimeSeconds"`
	LongestSeconds   float64       `json:"longestSeconds"`
	Count            int           `json:"count"`
	UptimePercent    *float64      `json:"uptimePercent"`
	ByCause          map[Cause]int `json:"byCause"`
}

// StatsFor calculates totals for the last n days, counting only monitored time.
func (s *Store) StatsFor(days int, now time.Time) Stats {
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	st := Stats{Days: days, ByCause: map[Cause]int{}}
	for _, o := range s.Outages(from) {
		start := o.Start
		if start.Before(from) {
			start = from
		}
		end := now
		if o.End != nil {
			end = *o.End
		}
		secs := end.Sub(start).Seconds()
		if secs < 0 {
			secs = 0
		}
		st.Count++
		st.ByCause[o.Cause]++
		st.DowntimeSeconds += secs
		if secs > st.LongestSeconds {
			st.LongestSeconds = secs
		}
	}
	st.MonitoredSeconds = s.monitoredSince(from, now)
	if st.DowntimeSeconds > st.MonitoredSeconds {
		// Outage times can exceed logged time just after startup.
		st.MonitoredSeconds = st.DowntimeSeconds
	}
	if st.MonitoredSeconds > 0 {
		pct := 100 * (1 - st.DowntimeSeconds/st.MonitoredSeconds)
		st.UptimePercent = &pct
	}
	return st
}

func (s *Store) monitoredSince(from, now time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := from.Local().Format("2006-01-02")
	total := 0.0
	for day, secs := range s.data.Days {
		if day >= first {
			total += secs
		}
	}
	if limit := now.Sub(from).Seconds(); total > limit {
		total = limit
	}
	return total
}

// DayCoverage returns monitored seconds for each local date since from.
func (s *Store) DayCoverage(from time.Time) map[string]float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := from.Local().Format("2006-01-02")
	out := map[string]float64{}
	for d, v := range s.data.Days {
		if d >= first {
			out[d] = v
		}
	}
	return out
}

// Clear deletes all outage and monitoring history.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.data.Outages[:0]
	for _, o := range s.data.Outages {
		if o.End == nil { // an outage in progress stays linked to the monitor
			kept = append(kept, o)
		}
	}
	s.data.Outages = kept
	s.data.Days = map[string]float64{}
	return s.saveLocked()
}

// NotifyEnabled reports whether outage-ended notifications are on.
func (s *Store) NotifyEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.data.NotifyOff
}

// SetNotify turns outage-ended notifications on or off.
func (s *Store) SetNotify(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.NotifyOff = !on
	return s.saveLocked()
}

// migrateOldData moves history from the app's previous name. It never overwrites
// existing DropTrace data.
func migrateOldData(oldDir, newDir string) error {
	if oldDir == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(newDir, "history.json")); err == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(oldDir, "history.json")); err != nil {
		return nil
	}
	if entries, err := os.ReadDir(newDir); err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("%s already has files", newDir)
		}
		if err := os.Remove(newDir); err != nil {
			return err
		}
	}
	return os.Rename(oldDir, newDir)
}
