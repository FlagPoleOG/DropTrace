package main

import (
	"sort"
	"sync"
	"time"
)

// Cause is where a failure most likely happened.
type Cause string

const (
	CauseNone    Cause = ""
	CausePC      Cause = "pc"
	CauseRouter  Cause = "router"
	CauseISP     Cause = "isp"
	CauseDNS     Cause = "dns"
	CauseUnclear Cause = "unclear"
)

// Timing rules. Checks are light enough to run every second while the window is
// open, and they run every few seconds from the tray.
const (
	VisibleInterval = 1 * time.Second
	TrayInterval    = 3 * time.Second
	failsToOpen     = 2
	oksToClose      = 2
	sleepGap        = 30 * time.Second // a longer pause between checks means the PC slept
	wakeGrace       = 30 * time.Second // networks need time to reconnect after waking
	startGrace      = 15 * time.Second // and after signing in to Windows
	noteSlept       = "The PC slept or the app paused during this outage. The end time is the last check before that."
	noteInterrupted = "The app or PC stopped during this outage. The end time is the last check before that."
)

// Sample is the result of one round of checks.
type Sample struct {
	Time       time.Time
	HasRoute   bool
	Gateway    string
	GatewayUp  bool
	GatewayMs  int // -1 when this check didn't time the router
	InternetUp bool
	DNSUp      bool
	LatencyMs  int // -1 when this check didn't time the internet
}

// CheckPoint is one check on the live graph.
type CheckPoint struct {
	T  time.Time
	Ms int
	OK bool
}

const recentPoints = 240

// Classify decides where a sample's failure most likely is. A router that has
// never answered tells us nothing, because many routers ignore these checks.
func Classify(s Sample, gatewayKnown bool) Cause {
	switch {
	case s.InternetUp && s.DNSUp:
		return CauseNone
	case s.InternetUp:
		return CauseDNS
	case !s.HasRoute:
		return CausePC
	case s.GatewayUp:
		return CauseISP
	case gatewayKnown:
		return CauseRouter
	default:
		return CauseUnclear
	}
}

// Status is what the interface shows.
type Status struct {
	State     string  `json:"state"` // starting, online, checking, outage
	Cause     Cause   `json:"cause"`
	Headline  string  `json:"headline"`
	Detail    string  `json:"detail"`
	Hops      []Hop   `json:"hops"`
	CheckedAt string  `json:"checkedAt"`
	Outage    *Outage `json:"outage,omitempty"`
}

// Hop is one stop on the path from this PC to the internet.
type Hop struct {
	Name   string `json:"name"`
	State  string `json:"state"` // up, down, unknown
	Detail string `json:"detail"`
}

// Monitor turns samples into outages.
type Monitor struct {
	mu         sync.Mutex
	store      *Store
	last       time.Time
	graceUntil time.Time
	failRun    int
	firstFail  time.Time
	firstCause Cause
	okRun      int
	firstOK    time.Time
	open       *Outage
	sample     Sample
	cause      Cause
	onClose    func(Outage)
	recent     []CheckPoint
}

// NewMonitor creates a monitor that stores outages in store.
func NewMonitor(store *Store) *Monitor { return &Monitor{store: store} }

// Step records one sample and returns the new status.
func (m *Monitor) Step(s Sample) Status {
	m.mu.Lock()
	var closed *Outage
	// Windows' monotonic clock stops during sleep, so compare wall-clock times.
	now := s.Time.Round(0)
	s.Time = now
	if s.GatewayUp && s.Gateway != "" {
		m.store.RememberGateway(s.Gateway)
	}
	cause := Classify(s, m.store.GatewayKnown(s.Gateway))
	delta := time.Duration(0)

	if m.last.IsZero() {
		m.graceUntil = now.Add(startGrace)
	} else {
		gap := now.Sub(m.last)
		if gap > sleepGap || gap < 0 {
			if m.open != nil {
				closed = m.closeOutage(m.last, noteSlept)
			}
			m.failRun, m.okRun = 0, 0
			m.graceUntil = now.Add(wakeGrace)
		} else {
			delta = gap
			m.store.AddMonitored(m.last, gap)
		}
	}
	m.last = now
	m.sample, m.cause = s, cause
	if len(m.recent) >= recentPoints {
		copy(m.recent, m.recent[1:])
		m.recent = m.recent[:recentPoints-1]
	}
	m.recent = append(m.recent, CheckPoint{T: now, Ms: s.LatencyMs, OK: s.InternetUp})

	if cause == CauseNone {
		m.failRun = 0
		if m.open != nil {
			m.okRun++
			if m.okRun == 1 {
				m.firstOK = now
			}
			m.store.TouchOutage(m.open, now)
			if m.okRun >= oksToClose {
				closed = m.closeOutage(m.firstOK, "")
			}
		}
	} else {
		m.okRun = 0
		switch {
		case m.open != nil:
			m.store.AddCauseTime(m.open, cause, delta)
			m.store.TouchOutage(m.open, now)
		case now.Before(m.graceUntil):
			// Still reconnecting, so this isn't an outage yet.
		default:
			m.failRun++
			if m.failRun == 1 {
				m.firstFail, m.firstCause = now, cause
			}
			if m.failRun >= failsToOpen {
				m.open = &Outage{
					Start:        m.firstFail,
					LastSeen:     now,
					Cause:        m.firstCause,
					Gateway:      s.Gateway,
					CauseSeconds: map[Cause]float64{m.firstCause: now.Sub(m.firstFail).Seconds()},
				}
				m.store.AddOutage(m.open)
				m.failRun = 0
			}
		}
	}
	st := m.statusLocked()
	onClose := m.onClose
	m.mu.Unlock()
	if closed != nil && onClose != nil {
		onClose(*closed)
	}
	return st
}

func (m *Monitor) closeOutage(end time.Time, note string) *Outage {
	o := m.store.CloseOutage(m.open, end, note)
	m.open, m.okRun = nil, 0
	return &o
}

// GraphScale picks the graph's top value from typical readings, so one slow reading
// after a reconnect doesn't flatten everything else.
func GraphScale(points []CheckPoint, now time.Time) int {
	var vals []int
	for _, p := range points {
		if p.OK && p.Ms >= 0 && now.Sub(p.T) <= 2*time.Minute {
			vals = append(vals, p.Ms)
		}
	}
	top := 0
	if len(vals) > 0 {
		sort.Ints(vals)
		top = vals[(len(vals)-1)*95/100]
	}
	for _, s := range []int{20, 50, 100, 200, 500, 1000, 2000} {
		if top*5/4 <= s {
			return s
		}
	}
	return 5000
}

// Recent appends the live graph's points to dst and returns it.
func (m *Monitor) Recent(dst []CheckPoint) []CheckPoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append(dst[:0], m.recent...)
}

// Status returns the latest status.
func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Monitor) statusLocked() Status {
	s := m.sample
	if m.last.IsZero() {
		return Status{
			State: "starting", Headline: "Running the first check.",
			Detail: "This takes a few seconds.",
			Hops: []Hop{
				{"This PC", "unknown", "Checking"}, {"Router", "unknown", "Checking"},
				{"Internet", "unknown", "Checking"}, {"Website lookups", "unknown", "Checking"},
			},
		}
	}
	st := Status{Cause: m.cause, Hops: m.hops(s), CheckedAt: s.Time.Format(time.RFC3339)}
	if m.open != nil {
		o := m.store.Snapshot(m.open)
		st.Outage = &o
	}
	info := CauseInfoFor(m.cause)
	switch {
	case m.cause == CauseNone && m.open != nil:
		st.State, st.Headline, st.Detail = "checking", "Your connection is coming back.", "DropTrace ends the outage after one more good check."
	case m.cause == CauseNone:
		st.State, st.Headline, st.Detail = "online", "Your internet is working.", "DropTrace will log any dropout in the background."
	case m.open != nil:
		st.State, st.Headline, st.Detail = "outage", info.Headline, info.Evidence
	case s.Time.Before(m.graceUntil):
		st.State, st.Headline, st.Detail = "checking", "Waiting for your network to reconnect.", "DropTrace allows a short time after startup or sleep before it logs a problem."
	default:
		st.State, st.Headline, st.Detail = "checking", "A check failed, so DropTrace is checking again.", info.Evidence
	}
	return st
}

func (m *Monitor) hops(s Sample) []Hop {
	h := []Hop{
		{"This PC", "up", "Connected to a network"},
		{"Router", "unknown", "Needs a network connection"},
		{"Internet", "unknown", "Needs a network connection"},
		{"Website lookups", "unknown", "Needs internet"},
	}
	if !s.HasRoute {
		h[0] = Hop{"This PC", "down", "Not connected to a network"}
		return h
	}
	switch {
	case s.GatewayUp:
		if s.GatewayMs >= 0 {
			h[1] = Hop{"Router", "up", "Answering in " + msText(s.GatewayMs)}
		} else {
			h[1] = Hop{"Router", "up", "Answering at " + s.Gateway}
		}
	case s.InternetUp:
		h[1].Detail = "Ignores these checks, which is normal for some routers"
	case m.store.GatewayKnown(s.Gateway):
		h[1] = Hop{"Router", "down", "Not answering at " + s.Gateway}
	default:
		h[1].Detail = "Hasn't answered yet, so DropTrace can't confirm it"
	}
	if s.InternetUp {
		detail := "Reachable"
		if s.LatencyMs >= 0 {
			detail = "Reachable in " + msText(s.LatencyMs)
		}
		h[2] = Hop{"Internet", "up", detail}
		if s.DNSUp {
			h[3] = Hop{"Website lookups", "up", "Working"}
		} else {
			h[3] = Hop{"Website lookups", "down", "Website names aren't being found"}
		}
	} else {
		h[2] = Hop{"Internet", "down", "None of 3 public servers answered"}
	}
	return h
}

func msText(ms int) string {
	if ms < 1 {
		return "under 1 ms"
	}
	return itoa(ms) + " ms"
}

// CauseInfo is the plain-English wording for a cause.
type CauseInfo struct{ Label, Headline, Evidence string }

// CauseInfoFor returns the wording used in the app and in reports.
func CauseInfoFor(c Cause) CauseInfo {
	switch c {
	case CauseISP:
		return CauseInfo{"Internet provider or modem", "Your internet provider or modem looks down.", "The router answered, but none of 3 public internet servers did."}
	case CauseRouter:
		return CauseInfo{"Router or home network", "Your PC can't reach your router.", "The router has answered before but stopped, and the internet is unreachable."}
	case CausePC:
		return CauseInfo{"This PC or its connection", "This PC isn't connected to a network.", "Windows has no route to the internet. Check the cable, adapter or Wi-Fi."}
	case CauseDNS:
		return CauseInfo{"Website lookups (DNS)", "Website lookups are failing.", "Internet servers answered, but website names couldn't be looked up."}
	case CauseUnclear:
		return CauseInfo{"Router or provider (unclear)", "Your internet is down, but the cause is unclear.", "Neither the router nor the internet answered, and this router has never answered checks."}
	}
	return CauseInfo{Label: "None"}
}
