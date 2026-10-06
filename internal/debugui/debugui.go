// Package debugui is a debugging web page for the collector: the state of each vehicle
// and the latest API calls, raw responses included, as well as duplicates and errors
// (which are not stored in the database). Everything is kept in memory.
//
// Counters (per endpoint, calls per API over the last 24 hours) cover the whole life of
// the process, not only the calls still in the ring buffer.
//
// The page shows VINs and locations: it is disabled by default and must listen
// locally only. It never shows a token (collector.Call does not contain one).
package debugui

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
	"sync"
	"time"

	"runsten/internal/collector"
	"runsten/internal/platform/clock"
	"runsten/internal/volvo"
)

//go:embed page.html
var pageHTML string

// quotaPerDay is the documented Volvo quota: 10,000 calls per day and per API. Whether
// failed calls count, and whether the day is rolling or calendar, is unknown: the page
// counts every call over a rolling window of hourly buckets, as an estimate.
const quotaPerDay = 10000

// endpointStats accumulates the calls of an endpoint since the process started.
type endpointStats struct {
	Endpoint            volvo.Endpoint
	Calls, Stored, Dups int
	Errors              int
	total               time.Duration
	Last                time.Time
	LastErr             string
}

func (s endpointStats) AvgMS() int64 {
	if s.Calls == 0 {
		return 0
	}
	return (s.total / time.Duration(s.Calls)).Milliseconds()
}

// Journal keeps the latest calls and the vehicle states. It implements
// collector.Observer and http.Handler.
type Journal struct {
	clk     clock.Clock
	page    *template.Template
	started time.Time

	mu       sync.Mutex
	calls    []collector.Call // ring buffer
	next     int
	full     bool
	vehicles map[string]collector.VehicleState
	stats    map[volvo.Endpoint]*endpointStats
	hourly   map[string]map[time.Time]int // API → hour → calls
}

// New creates a journal of the last size calls.
func New(size int, clk clock.Clock) *Journal {
	j := &Journal{
		clk: clk, started: clk.Now(), calls: make([]collector.Call, size),
		vehicles: map[string]collector.VehicleState{},
		stats:    map[volvo.Endpoint]*endpointStats{},
		hourly:   map[string]map[time.Time]int{},
	}
	j.page = template.Must(template.New("page").Funcs(template.FuncMap{
		"clock":  func(t time.Time) string { return t.UTC().Format("15:04:05") },
		"until":  j.until,
		"ago":    j.ago,
		"pretty": pretty,
		"ms":     func(d time.Duration) int64 { return d.Milliseconds() },
	}).Parse(pageHTML))
	return j
}

// Called implements collector.Observer.
func (j *Journal) Called(c collector.Call) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.calls[j.next] = c
	j.next = (j.next + 1) % len(j.calls)
	j.full = j.full || j.next == 0

	st := j.stats[c.Endpoint]
	if st == nil {
		st = &endpointStats{Endpoint: c.Endpoint}
		j.stats[c.Endpoint] = st
	}
	st.Calls++
	st.total += c.Duration
	st.Last = c.At
	switch {
	case c.Err != "":
		st.Errors++
		st.LastErr = c.Err
	case c.Stored:
		st.Stored++
	default:
		st.Dups++
	}

	api := c.Endpoint.API()
	hours := j.hourly[api]
	if hours == nil {
		hours = map[time.Time]int{}
		j.hourly[api] = hours
	}
	hours[c.At.Truncate(time.Hour)]++
	for h := range hours {
		if c.At.Sub(h) >= 24*time.Hour {
			delete(hours, h)
		}
	}
}

// Observed implements collector.Observer.
func (j *Journal) Observed(v collector.VehicleState) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.vehicles[v.VehicleID] = v
}

// recent returns the filtered calls, from newest to oldest.
func (j *Journal) recent(keep func(collector.Call) bool) []collector.Call {
	j.mu.Lock()
	defer j.mu.Unlock()
	n := j.next
	if j.full {
		n = len(j.calls)
	}
	out := make([]collector.Call, 0, n)
	for i := range n {
		c := j.calls[(j.next-1-i+len(j.calls))%len(j.calls)]
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

func (j *Journal) states() []collector.VehicleState {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]collector.VehicleState, 0, len(j.vehicles))
	for _, v := range j.vehicles {
		out = append(out, v)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].VIN < out[b].VIN })
	return out
}

func (j *Journal) until(t time.Time) string {
	d := t.Sub(j.clk.Now())
	if d <= 0 {
		return "due"
	}
	return d.Round(time.Second).String()
}

func (j *Journal) ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := j.clk.Now().Sub(t).Round(time.Second)
	if d <= 0 {
		return "just now"
	}
	return d.String() + " ago"
}

type quotaView struct {
	API     string
	Used    int
	Limit   int
	Percent int
}

type totals struct {
	Calls, Stored, Dups, Errors int
	ErrorRate                   int // percent
}

// counters returns the per endpoint stats, the totals and the calls per API over the
// last 24 hours.
func (j *Journal) counters() ([]endpointStats, totals, []quotaView) {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := j.clk.Now()
	var eps []endpointStats
	var t totals
	for _, s := range j.stats {
		eps = append(eps, *s)
		t.Calls += s.Calls
		t.Stored += s.Stored
		t.Dups += s.Dups
		t.Errors += s.Errors
	}
	sort.Slice(eps, func(a, b int) bool { return eps[a].Endpoint < eps[b].Endpoint })
	if t.Calls > 0 {
		t.ErrorRate = t.Errors * 100 / t.Calls
	}
	var qs []quotaView
	for _, api := range []string{volvo.APIConnectedVehicle, volvo.APIEnergy, volvo.APILocation} {
		q := quotaView{API: api, Limit: quotaPerDay}
		for h, n := range j.hourly[api] {
			if now.Sub(h) < 24*time.Hour {
				q.Used += n
			}
		}
		q.Percent = min(100, q.Used*100/quotaPerDay)
		qs = append(qs, q)
	}
	return eps, t, qs
}

func pretty(raw []byte) string {
	var b bytes.Buffer
	if json.Indent(&b, raw, "", "  ") != nil {
		return string(raw)
	}
	return b.String()
}

type nextDue struct {
	Endpoint volvo.Endpoint
	At       time.Time
}

type vehicleView struct {
	collector.VehicleState
	Due []nextDue
}

type pageData struct {
	Now       time.Time
	Started   time.Time
	LastPass  time.Time
	Totals    totals
	Quotas    []quotaView
	Stats     []endpointStats
	Vehicles  []vehicleView
	Calls     []collector.Call
	Endpoints []volvo.Endpoint
	Filter    struct{ Vehicle, Endpoint string }
	Errors    bool
	Paused    bool
	Total     int
}

// ServeHTTP serves the page. Filters: ?vehicle=<id>, ?endpoint=<name>, ?errors=1;
// ?pause=1 disables auto-refresh.
func (j *Journal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	var d pageData
	d.Now, d.Started = j.clk.Now(), j.started
	d.Stats, d.Totals, d.Quotas = j.counters()
	d.Filter.Vehicle, d.Filter.Endpoint = q.Get("vehicle"), q.Get("endpoint")
	d.Errors, d.Paused = q.Get("errors") == "1", q.Get("pause") == "1"

	for _, v := range j.states() {
		if v.At.After(d.LastPass) {
			d.LastPass = v.At
		}
		vv := vehicleView{VehicleState: v}
		for ep, at := range v.Next {
			vv.Due = append(vv.Due, nextDue{ep, at})
		}
		sort.Slice(vv.Due, func(a, b int) bool { return vv.Due[a].At.Before(vv.Due[b].At) })
		d.Vehicles = append(d.Vehicles, vv)
	}
	seen := map[volvo.Endpoint]bool{}
	all := j.recent(func(collector.Call) bool { return true })
	d.Total = len(all)
	for _, c := range all {
		if !seen[c.Endpoint] {
			seen[c.Endpoint] = true
			d.Endpoints = append(d.Endpoints, c.Endpoint)
		}
		if (d.Filter.Vehicle == "" || c.VehicleID == d.Filter.Vehicle) &&
			(d.Filter.Endpoint == "" || string(c.Endpoint) == d.Filter.Endpoint) &&
			(!d.Errors || c.Err != "") {
			d.Calls = append(d.Calls, c)
		}
	}
	sort.Slice(d.Endpoints, func(a, b int) bool { return d.Endpoints[a] < d.Endpoints[b] })

	var buf bytes.Buffer
	if err := j.page.Execute(&buf, d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	_, _ = buf.WriteTo(w)
}
