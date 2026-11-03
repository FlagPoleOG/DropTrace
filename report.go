package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ReportOptions are the choices made in the Report view.
type ReportOptions struct {
	Name, Provider, Account string
	Days                    int
	ProviderOnly            bool
}

// ReportRow is one outage in a report.
type ReportRow struct{ Start, End, Duration, Cause, Evidence, Note string }

// ReportData is everything shown in a report.
type ReportData struct {
	ReportOptions
	Generated, From, To                  string
	Rows                                 []ReportRow
	Count, Other                         int
	Downtime, Longest, Monitored, Uptime string
}

// BuildReport gathers one period's outages.
func BuildReport(store *Store, opt ReportOptions, now time.Time) ReportData {
	from := now.Add(-time.Duration(opt.Days) * 24 * time.Hour)
	d := ReportData{ReportOptions: opt, Generated: now.Format("January 2, 2006 3:04 PM MST"),
		From: from.Format("January 2, 2006"), To: now.Format("January 2, 2006")}
	var down, longest float64
	for _, o := range store.Outages(from) {
		if opt.ProviderOnly && o.Cause != CauseISP {
			d.Other++
			continue
		}
		start := o.Start
		if start.Before(from) {
			start = from
		}
		end, endText := now, "Still down"
		if o.End != nil {
			end, endText = *o.End, o.End.Local().Format("3:04:05 PM")
			if o.End.Local().Format("2006-01-02") != o.Start.Local().Format("2006-01-02") {
				endText = o.End.Local().Format("Jan 2, 3:04:05 PM")
			}
		}
		secs := end.Sub(start).Seconds()
		if secs < 0 {
			secs = 0
		}
		down += secs
		if secs > longest {
			longest = secs
		}
		info := CauseInfoFor(o.Cause)
		d.Rows = append(d.Rows, ReportRow{Start: o.Start.Local().Format("Mon, Jan 2, 2006, 3:04:05 PM"), End: endText,
			Duration: formatDuration(o.Duration(now)), Cause: info.Label, Evidence: info.Evidence, Note: o.Note})
	}
	d.Count = len(d.Rows)
	d.Downtime = formatDuration(time.Duration(down) * time.Second)
	d.Longest = formatDuration(time.Duration(longest) * time.Second)
	mon := store.monitoredSince(from, now)
	if down > mon {
		mon = down
	}
	d.Monitored = formatDuration(time.Duration(mon) * time.Second)
	d.Uptime = "Not enough data"
	if mon > 0 {
		d.Uptime = fmt.Sprintf("%.2f%%", 100*(1-down/mon))
	}
	return d
}

// WriteCSV exports one period's outages.
func WriteCSV(w io.Writer, store *Store, days int, now time.Time) error {
	c := csv.NewWriter(w)
	_ = c.Write([]string{"Start", "End", "Duration (seconds)", "Likely cause", "Evidence", "Note"})
	for _, o := range store.Outages(now.Add(-time.Duration(days) * 24 * time.Hour)) {
		end := "Still down"
		if o.End != nil {
			end = o.End.Local().Format(time.RFC3339)
		}
		info := CauseInfoFor(o.Cause)
		_ = c.Write([]string{o.Start.Local().Format(time.RFC3339), end,
			strconv.Itoa(int(o.Duration(now).Seconds())), info.Label, info.Evidence, o.Note})
	}
	c.Flush()
	return c.Error()
}

// ---- PDF ----

const (
	pageW, pageH = 612.0, 792.0
	marginX      = 54.0
	marginTop    = 54.0
	marginBottom = 54.0
)

// Helvetica widths for characters 32 to 126, per 1000 points of font size.
var helvWidths = []int{278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015,
	667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611,
	278, 278, 278, 469, 556, 333,
	556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500,
	334, 260, 334, 584}

func textWidth(s string, size float64) float64 {
	w := 0
	for _, r := range s {
		if r >= 32 && r <= 126 {
			w += helvWidths[r-32]
		} else {
			w += 556
		}
	}
	return float64(w) * size / 1000
}

func wrapText(s string, size, width float64) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		next := word
		if line != "" {
			next = line + " " + word
		}
		if textWidth(next, size) > width && line != "" {
			lines = append(lines, line)
			line = word
		} else {
			line = next
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func pdfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r >= 32 && r <= 126:
			b.WriteRune(r)
		case r >= 160 && r <= 255: // the same bytes in WinAnsi
			b.WriteByte(byte(r))
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

type pdfDoc struct {
	pages []string
	cur   strings.Builder
	y     float64
}

func (p *pdfDoc) newPage() {
	if p.cur.Len() > 0 {
		p.pages = append(p.pages, p.cur.String())
		p.cur.Reset()
	}
	p.y = pageH - marginTop
}

func (p *pdfDoc) need(h float64) bool {
	if p.y-h < marginBottom {
		p.newPage()
		return true
	}
	return false
}

func (p *pdfDoc) text(x, y float64, font string, size float64, grey float64, s string) {
	fmt.Fprintf(&p.cur, "BT /%s %.1f Tf %.2f g %.2f %.2f Td (%s) Tj ET\n", font, size, grey, x, y, pdfEscape(s))
}

func (p *pdfDoc) rule(y, weight float64) {
	fmt.Fprintf(&p.cur, "%.2f w 0.75 G %.2f %.2f m %.2f %.2f l S\n", weight, marginX, y, pageW-marginX, y)
}

func (p *pdfDoc) para(x, width float64, font string, size, leading, grey float64, s string) {
	for _, l := range wrapText(s, size, width) {
		p.need(leading)
		p.text(x, p.y-size, font, size, grey, l)
		p.y -= leading
	}
}

// RenderPDF lays out the report on US Letter pages using standard PDF fonts.
func RenderPDF(d ReportData) []byte {
	p := &pdfDoc{}
	p.newPage()
	full := pageW - 2*marginX
	p.para(marginX, full, "F2", 22, 28, 0.08, "Internet outage report")
	p.para(marginX, full, "F1", 10.5, 15, 0.35, fmt.Sprintf("%s to %s. Created %s.", d.From, d.To, d.Generated))
	p.y -= 6
	for _, kv := range [][2]string{{"Customer", d.Name}, {"Internet provider", d.Provider}, {"Account", d.Account}} {
		if strings.TrimSpace(kv[1]) != "" {
			p.para(marginX, full, "F1", 10.5, 15, 0.1, kv[0]+": "+kv[1])
		}
	}
	p.y -= 8
	p.rule(p.y, 0.75)
	p.y -= 12
	label := "outages"
	if d.ProviderOnly {
		label = "provider-side outages"
	}
	figs := [][2]string{{itoa(d.Count), label}, {d.Downtime, "total downtime"}, {d.Longest, "longest outage"}, {d.Uptime, "available while monitored"}}
	for i, f := range figs {
		x := marginX + float64(i)*full/4
		size := 17.0
		if textWidth(f[0], size)*1.05 > full/4-8 {
			size = 11
		}
		p.text(x, p.y-17, "F2", size, 0.08, f[0])
		p.text(x, p.y-31, "F1", 9, 0.35, f[1])
	}
	p.y -= 42
	p.rule(p.y, 0.75)
	p.y -= 22

	title := "All outages"
	if d.ProviderOnly {
		title = "Outages beyond the home router"
	}
	p.para(marginX, full, "F2", 13, 20, 0.08, title)
	p.y -= 2
	cols := []float64{marginX, marginX + 142, marginX + 226, marginX + 276}
	lastW := pageW - marginX - cols[3]
	header := func() {
		p.text(cols[0], p.y-10, "F2", 9.5, 0.08, "Started")
		p.text(cols[1], p.y-10, "F2", 9.5, 0.08, "Ended")
		p.text(cols[2], p.y-10, "F2", 9.5, 0.08, "Length")
		h := "Likely cause"
		if d.ProviderOnly {
			h = "Evidence"
		}
		p.text(cols[3], p.y-10, "F2", 9.5, 0.08, h)
		p.y -= 15
		p.rule(p.y, 1)
		p.y -= 3
	}
	if len(d.Rows) == 0 {
		msg := "No outages were logged in this period."
		if d.ProviderOnly {
			msg = "No provider-side outages were logged in this period."
		}
		p.para(marginX, full, "F1", 10.5, 15, 0.1, msg)
	} else {
		header()
		for _, r := range d.Rows {
			detail := r.Evidence
			if !d.ProviderOnly {
				detail = r.Cause + ". " + r.Evidence
			}
			if r.Note != "" {
				detail += " " + r.Note
			}
			started := wrapText(r.Start, 9.5, cols[1]-cols[0]-8)
			ended := wrapText(r.End, 9.5, cols[2]-cols[1]-8)
			lines := wrapText(detail, 9.5, lastW)
			rows := max(len(started), len(ended), len(lines))
			h := float64(rows)*12.5 + 7
			if p.need(h) {
				header()
			}
			top := p.y - 12
			for i, l := range started {
				p.text(cols[0], top-float64(i)*12.5, "F1", 9.5, 0.1, l)
			}
			for i, l := range ended {
				p.text(cols[1], top-float64(i)*12.5, "F1", 9.5, 0.1, l)
			}
			p.text(cols[2], top, "F1", 9.5, 0.1, r.Duration)
			for i, l := range lines {
				p.text(cols[3], top-float64(i)*12.5, "F1", 9.5, 0.1, l)
			}
			p.y -= h
			p.rule(p.y+1, 0.5)
		}
	}
	p.y -= 18
	p.need(110)
	p.para(marginX, full, "F2", 13, 20, 0.08, "How this was measured")
	method := "DropTrace ran on a PC at this location for " + d.Monitored + " of this period and checked the connection every few seconds. " +
		"Checks tested the PC's network connection, the home router, and public internet servers (1.1.1.1, 8.8.8.8 and 9.9.9.9). " +
		"An outage is listed as provider-side when the router answered but none of the public servers did. " +
		"That points to the modem or the provider's network, but it doesn't identify the exact fault. Time when the PC was off or asleep isn't measured."
	if d.ProviderOnly && d.Other > 0 {
		method += " Other outages left out because the evidence pointed elsewhere or was unclear: " + itoa(d.Other) + "."
	}
	p.para(marginX, full, "F1", 10, 14, 0.3, method)
	p.newPage()
	return assemblePDF(p.pages)
}

func assemblePDF(pages []string) []byte {
	if len(pages) == 0 {
		pages = []string{""}
	}
	var b strings.Builder
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	b.WriteString("%PDF-1.4\n")
	kids := make([]string, len(pages))
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 5+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	for i, content := range pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>", pageW, pageH, 6+2*i))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return []byte(b.String())
}
