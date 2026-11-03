//go:build windows && !droptrace_sim

package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"net"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

type winNet struct {
	dnsBusy [3]atomic.Bool
}

func newNetOps() NetOps { return &winNet{} }

var publicTargets = []struct{ ip, sni string }{
	{"1.1.1.1", "one.one.one.one"},
	{"8.8.8.8", "dns.google"},
	{"9.9.9.9", "dns.quad9.net"},
}

var dnsNames = [3]string{"www.microsoft.com", "www.cloudflare.com", "dns.google"}

// Route asks Windows which router it would use to reach the internet.
func (*winNet) Route() (string, bool) { return bestGateway() }

// Ping sends one echo request.
func (*winNet) Ping(ip string, timeout time.Duration) (bool, int) { return ping(ip, timeout) }

// GatewayTCP tests routers that ignore ping.
func (*winNet) GatewayTCP(ctx context.Context, gw string) bool {
	d := net.Dialer{Timeout: 1200 * time.Millisecond}
	for _, port := range []string{"80", "443"} {
		c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(gw, port))
		if err == nil {
			c.Close()
			return true
		}
		if errors.Is(err, syscall.Errno(10061)) { // WSAECONNREFUSED: refused means something is there
			return true
		}
	}
	return false
}

// bestGateway asks Windows which router it would use to reach the internet.
func bestGateway() (string, bool) {
	var row [14]uint32 // MIB_IPFORWARDROW
	dest := binary.LittleEndian.Uint32(net.IPv4(8, 8, 8, 8).To4())
	r, _, _ := procGetBestRoute.Call(uintptr(dest), 0, uintptr(unsafe.Pointer(&row[0])))
	if r != 0 {
		return "", false
	}
	if !interfaceConnected(row[4]) {
		// Windows can keep a route for a while after a cable is unplugged.
		return "", false
	}
	if row[3] == 0 { // no next hop: directly connected, such as some VPNs
		return "", true
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], row[3])
	return net.IP(b[:]).String(), true
}

// interfaceConnected reports whether the route's adapter still has a live link. If
// Windows can't say, it's assumed connected so the other checks decide.
func interfaceConnected(index uint32) bool {
	const (
		rowSize         = 1352 // MIB_IF_ROW2 on 64-bit Windows
		indexOffset     = 8
		operOffset      = 1156
		mediaOffset     = 1164
		operUp          = 1
		mediaDisconnect = 2
	)
	row := make([]byte, rowSize)
	binary.LittleEndian.PutUint32(row[indexOffset:], index)
	if r, _, _ := procGetIfEntry2.Call(uintptr(unsafe.Pointer(&row[0]))); r != 0 {
		return true
	}
	oper := binary.LittleEndian.Uint32(row[operOffset:])
	media := binary.LittleEndian.Uint32(row[mediaOffset:])
	return oper == operUp && media != mediaDisconnect
}

// ping uses the Windows ICMP API, which doesn't need administrator rights.
func ping(ip string, timeout time.Duration) (bool, int) {
	v4 := net.ParseIP(ip).To4()
	if v4 == nil {
		return false, 0
	}
	h, _, _ := procIcmpCreateFile.Call()
	if h == 0 || h == ^uintptr(0) {
		return false, 0
	}
	defer procIcmpCloseHandle.Call(h)
	payload := []byte("droptrace")
	reply := make([]byte, 128)
	n, _, _ := procIcmpSendEcho.Call(h, uintptr(binary.LittleEndian.Uint32(v4)),
		uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout.Milliseconds()))
	if n == 0 {
		return false, 0
	}
	// ICMP_ECHO_REPLY: Address (0), Status (4), RoundTripTime (8)
	if binary.LittleEndian.Uint32(reply[4:8]) != 0 {
		return false, 0
	}
	return true, int(binary.LittleEndian.Uint32(reply[8:12]))
}

// PublicTLS needs a finished handshake. A plain TCP connection isn't enough, because
// filtering software such as ad blockers can accept it locally.
func (*winNet) PublicTLS(ctx context.Context) (bool, int) {
	type result struct {
		ok bool
		ms int
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	results := make(chan result, len(publicTargets))
	for _, t := range publicTargets {
		go func(ip, sni string) {
			start := time.Now()
			d := tls.Dialer{
				NetDialer: &net.Dialer{Timeout: 3 * time.Second},
				// Only reachability is tested and nothing is sent, so the certificate
				// doesn't need checking. HTTPS filtering software may replace it.
				Config: &tls.Config{ServerName: sni, InsecureSkipVerify: true},
			}
			c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
			if err != nil {
				results <- result{}
				return
			}
			c.Close()
			results <- result{true, int(time.Since(start).Milliseconds())}
		}(t.ip, t.sni)
	}
	for range publicTargets {
		select {
		case r := <-results:
			if r.ok {
				return true, r.ms
			}
		case <-ctx.Done():
			return false, -1
		}
	}
	return false, -1
}

// DNS bypasses the Windows cache, which could hide a failure during an outage.
func (p *winNet) DNS(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	results := make(chan bool, len(dnsNames))
	for i := range dnsNames {
		// DnsQuery can't be cancelled, so never stack up more than one per name.
		if !p.dnsBusy[i].CompareAndSwap(false, true) {
			results <- false
			continue
		}
		go func(i int) {
			defer p.dnsBusy[i].Store(false)
			results <- dnsQuery(dnsNames[i])
		}(i)
	}
	for range dnsNames {
		select {
		case ok := <-results:
			if ok {
				return true
			}
		case <-ctx.Done():
			return false
		}
	}
	return false
}

func dnsQuery(name string) bool {
	const (
		dnsTypeA          = 1
		dnsBypassCache    = 0x8
		dnsFreeRecordList = 1
	)
	n, _ := syscall.UTF16PtrFromString(name)
	var records uintptr
	r, _, _ := procDnsQueryW.Call(uintptr(unsafe.Pointer(n)), dnsTypeA, dnsBypassCache, 0,
		uintptr(unsafe.Pointer(&records)), 0)
	if records != 0 {
		_, _, _ = procDnsRecordListFree.Call(records, dnsFreeRecordList)
	}
	return r == 0
}
