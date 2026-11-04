package main

import (
	"context"
	"sync"
	"time"
)

// NetOps are the operating system's network tools.
type NetOps interface {
	Route() (gateway string, ok bool)
	Ping(ip string, timeout time.Duration) (bool, int)
	LinkDown() bool
	GatewayTCP(ctx context.Context, gateway string) bool
	PublicTLS(ctx context.Context) (bool, int)
	DNS(ctx context.Context) bool
}

var publicIPs = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}

const icmpTimedOut = 11010 // IP_REQ_TIMED_OUT

// pingAnswerInvalid spots a "timed out" answer that came back far too soon. Windows
// sometimes reports this within milliseconds, which can't be a real timeout, so it says
// nothing about whether a packet was lost.
func pingAnswerInvalid(code uintptr, took, wait time.Duration) bool {
	limit := 100 * time.Millisecond
	if wait/4 < limit {
		limit = wait / 4
	}
	return code == icmpTimedOut && took < limit
}

const (
	fullCheckEvery = 30 * time.Second
	noPingEvery    = 5 * time.Second
	fastPingWait   = 900 * time.Millisecond
	fullPingWait   = 1500 * time.Millisecond
)

// Checker keeps healthy checks light. Normally it sends two pings. It runs
// lookups and fallback tests only when something fails, or every so often.
type Checker struct {
	net       NetOps
	next      int
	lastFull  time.Time
	gateway   string
	gatewayUp bool
	dnsUp     bool
	last      Sample
	failing   bool
	noPing    bool
}

// NewChecker creates a checker that uses the given network tools.
func NewChecker(n NetOps) *Checker { return &Checker{net: n} }

// Check runs one round. now is passed in so tests can control time.
func (c *Checker) Check(ctx context.Context, now time.Time) Sample {
	gw, ok := c.net.Route()
	if !ok {
		c.lastFull, c.failing = time.Time{}, true
		return Sample{GatewayMs: -1, LatencyMs: -1, LinkDown: c.net.LinkDown()}
	}
	since := now.Sub(c.lastFull)
	full := c.lastFull.IsZero() || c.failing || gw != c.gateway || since >= fullCheckEvery || since < 0

	if !full && c.noPing {
		if since < noPingEvery {
			s := c.last
			s.GatewayMs, s.LatencyMs = -1, -1
			return s
		}
		full = true
	}
	if !full {
		if s, ok := c.fast(gw); ok {
			return s
		}
	}
	return c.fullCheck(ctx, gw, now)
}

func (c *Checker) fast(gw string) (Sample, bool) {
	s := Sample{HasRoute: true, Gateway: gw, GatewayUp: c.gatewayUp, DNSUp: c.dnsUp, GatewayMs: -1, LatencyMs: -1}
	target := publicIPs[c.next%len(publicIPs)]
	c.next++
	var gwOK bool
	var gwMs int
	var wg sync.WaitGroup
	if gw != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gwOK, gwMs = c.net.Ping(gw, fastPingWait)
		}()
	}
	pubOK, pubMs := c.net.Ping(target, fastPingWait)
	wg.Wait()
	if !pubOK {
		return s, false
	}
	s.InternetUp, s.LatencyMs = true, pubMs
	if gwOK {
		s.GatewayUp, s.GatewayMs = true, gwMs
		c.gatewayUp = true
	}
	c.last = s
	return s, true
}

func (c *Checker) fullCheck(ctx context.Context, gw string, now time.Time) Sample {
	s := Sample{HasRoute: true, Gateway: gw, GatewayMs: -1, LatencyMs: -1}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if gw == "" {
			return
		}
		if ok, ms := c.net.Ping(gw, fullPingWait); ok {
			s.GatewayUp, s.GatewayMs = true, ms
		} else {
			s.GatewayUp = c.net.GatewayTCP(ctx, gw)
		}
	}()
	go func() {
		defer wg.Done()
		s.DNSUp = c.net.DNS(ctx)
	}()

	// Any public server answering a ping is enough. TLS is only the fallback.
	type reply struct {
		ok bool
		ms int
	}
	replies := make(chan reply, len(publicIPs))
	for _, ip := range publicIPs {
		go func(ip string) {
			ok, ms := c.net.Ping(ip, fullPingWait)
			replies <- reply{ok, ms}
		}(ip)
	}
	pinged := false
	for range publicIPs {
		if r := <-replies; r.ok {
			pinged = true
			s.InternetUp, s.LatencyMs = true, r.ms
			break
		}
	}
	if !pinged {
		// A handshake takes far longer than a ping, so it isn't graphed as response time.
		s.InternetUp, _ = c.net.PublicTLS(ctx)
	}
	wg.Wait()

	c.noPing = s.InternetUp && !pinged
	c.lastFull, c.gateway, c.gatewayUp, c.dnsUp = now, gw, s.GatewayUp, s.DNSUp
	c.failing = !s.InternetUp || !s.DNSUp
	c.last = s
	return s
}
