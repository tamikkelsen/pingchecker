// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Tommy Mikkelsen
// Developed by Tommy Mikkelsen in collaboration with Claude (Anthropic's Claude Code).
// Licensed under the MIT License; see the LICENSE file for details.

// PingChecker — multi-host latency monitor (Go edition).
//
// A single self-contained binary: no runtime dependencies, embeds the web
// dashboard, stores results in a local SQLite file, and streams live pings to
// the browser over WebSocket. Cross-compiles for Windows, macOS, and Linux.
//
//	Build:  go build -o pingchecker .
//	Run:    ./pingchecker
//	Open:   http://localhost:8765
package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

//go:embed static
var staticFS embed.FS

// Bind to loopback only: the API and WebSocket are unauthenticated, so the
// server is restricted to the local machine. Change to "0.0.0.0:8765" only if
// you intend to expose it on a trusted LAN.
const addr = "127.0.0.1:8765"

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

// ---------------------------------------------------------------------------
// Paths — data files live next to the binary (overridable via PINGCHECKER_DATA)
// ---------------------------------------------------------------------------

func dataDir() string {
	if d := os.Getenv("PINGCHECKER_DATA"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		// `go run` builds into a temp dir — don't scatter data there.
		if !strings.Contains(dir, "go-build") {
			return dir
		}
	}
	wd, _ := os.Getwd()
	return wd
}

var (
	dbPath     string
	configPath string
	db         *sql.DB
)

// ---------------------------------------------------------------------------
// Settings — runtime-adjustable, persisted in the DB, edited live from the UI
// ---------------------------------------------------------------------------

type Settings struct {
	Interval   int `json:"interval"`    // seconds between ping rounds
	PacketSize int `json:"packet_size"` // ICMP payload bytes (0 = OS default)
	TimeoutMs  int `json:"timeout_ms"`  // per-ping timeout in milliseconds
	Paused     int `json:"paused"`      // 1 = pause the background loop
}

var defaultSettings = Settings{Interval: 1, PacketSize: 56, TimeoutMs: 2000, Paused: 0}

var (
	settingsMu sync.RWMutex
	settings   = defaultSettings
)

func getSettings() Settings {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	return settings
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// settingsIn is the partial-update payload for PUT /api/settings.
type settingsIn struct {
	Interval   *int  `json:"interval"`
	PacketSize *int  `json:"packet_size"`
	TimeoutMs  *int  `json:"timeout_ms"`
	Paused     *bool `json:"paused"`
}

// applyUpdate clamps and applies a partial settings update; returns true if
// anything changed.
func (in settingsIn) applyUpdate(s *Settings) bool {
	changed := false
	if in.Interval != nil {
		s.Interval = clampInt(*in.Interval, 1, 3600)
		changed = true
	}
	if in.PacketSize != nil {
		s.PacketSize = clampInt(*in.PacketSize, 0, 65500)
		changed = true
	}
	if in.TimeoutMs != nil {
		s.TimeoutMs = clampInt(*in.TimeoutMs, 100, 60000)
		changed = true
	}
	if in.Paused != nil {
		if *in.Paused {
			s.Paused = 1
		} else {
			s.Paused = 0
		}
		changed = true
	}
	return changed
}

// ---------------------------------------------------------------------------
// Database
// ---------------------------------------------------------------------------

func openDB() error {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		dbPath,
	)
	var err error
	db, err = sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	// SQLite has a single writer; one connection keeps reads/writes serialized
	// and avoids "database is locked" under this app's light load.
	db.SetMaxOpenConns(1)
	return db.Ping()
}

func initDB(ctx context.Context) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS hosts (
			host      TEXT PRIMARY KEY,
			label     TEXT NOT NULL,
			added_at  INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS pings (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			host       TEXT    NOT NULL,
			timestamp  INTEGER NOT NULL,
			success    INTEGER NOT NULL,
			latency_ms REAL
		);
		CREATE INDEX IF NOT EXISTS idx_pt ON pings(host, timestamp);
		CREATE INDEX IF NOT EXISTS idx_t  ON pings(timestamp);
		CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	return err
}

// ---------------------------------------------------------------------------
// Config (config.json) — first-run defaults for hosts and settings
// ---------------------------------------------------------------------------

type configFile struct {
	Hosts    []json.RawMessage          `json:"hosts"`
	Settings map[string]json.RawMessage `json:"settings"`
}

func readConfig() (*configFile, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg configFile
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func seedHosts(ctx context.Context) error {
	cfg, err := readConfig()
	if err != nil {
		return nil // no config / unreadable → nothing to seed
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM hosts").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // already seeded — don't overwrite user changes
	}

	now := time.Now().Unix()
	for _, entry := range cfg.Hosts {
		host, label := "", ""
		var asStr string
		if json.Unmarshal(entry, &asStr) == nil {
			host, label = asStr, asStr
		} else {
			var obj struct {
				Host  string `json:"host"`
				Label string `json:"label"`
			}
			if json.Unmarshal(entry, &obj) != nil || obj.Host == "" {
				continue
			}
			host = obj.Host
			label = obj.Label
			if label == "" {
				label = host
			}
		}
		if _, err := db.ExecContext(ctx,
			"INSERT OR IGNORE INTO hosts (host,label,added_at) VALUES (?,?,?)",
			host, label, now); err != nil {
			return err
		}
	}
	return nil
}

func persistSettings(ctx context.Context) error {
	s := getSettings()
	kv := map[string]int{
		"interval":    s.Interval,
		"packet_size": s.PacketSize,
		"timeout_ms":  s.TimeoutMs,
		"paused":      s.Paused,
	}
	for k, v := range kv {
		if _, err := db.ExecContext(ctx,
			"INSERT OR REPLACE INTO settings (key,value) VALUES (?,?)",
			k, strconv.Itoa(v)); err != nil {
			return err
		}
	}
	return nil
}

// loadSettings populates settings from defaults ← config.json ← persisted rows.
func loadSettings(ctx context.Context) error {
	merged := defaultSettings

	if cfg, err := readConfig(); err == nil {
		in := settingsIn{}
		if r, ok := cfg.Settings["interval"]; ok {
			var v int
			if json.Unmarshal(r, &v) == nil {
				in.Interval = &v
			}
		}
		if r, ok := cfg.Settings["packet_size"]; ok {
			var v int
			if json.Unmarshal(r, &v) == nil {
				in.PacketSize = &v
			}
		}
		if r, ok := cfg.Settings["timeout_ms"]; ok {
			var v int
			if json.Unmarshal(r, &v) == nil {
				in.TimeoutMs = &v
			}
		}
		in.applyUpdate(&merged)
	}

	rows, err := db.QueryContext(ctx, "SELECT key,value FROM settings")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		switch k {
		case "interval":
			merged.Interval = n
		case "packet_size":
			merged.PacketSize = n
		case "timeout_ms":
			merged.TimeoutMs = n
		case "paused":
			merged.Paused = n
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	settingsMu.Lock()
	settings = merged
	settingsMu.Unlock()
	return persistSettings(ctx)
}

// ---------------------------------------------------------------------------
// Ping — shell out to the system `ping` (no raw sockets, no root needed)
// ---------------------------------------------------------------------------

var timeRe = regexp.MustCompile(`time[=<](\d+\.?\d*)\s*ms`)

// pingOnce pings host once; returns (reachable, rtt_ms or nil).
func pingOnce(ctx context.Context, host string, size, timeoutMs int) (bool, *float64) {
	var args []string
	switch runtime.GOOS {
	case "windows":
		args = []string{"-n", "1", "-w", strconv.Itoa(timeoutMs)}
		if size > 0 {
			args = append(args, "-l", strconv.Itoa(size)) // -l sets payload size
		}
	case "darwin": // -W is milliseconds on macOS
		args = []string{"-c", "1", "-W", strconv.Itoa(timeoutMs)}
		if size > 0 {
			args = append(args, "-s", strconv.Itoa(size)) // -s sets payload size
		}
	default: // Linux: -W is seconds
		secs := (timeoutMs + 999) / 1000
		if secs < 1 {
			secs = 1
		}
		args = []string{"-c", "1", "-W", strconv.Itoa(secs)}
		if size > 0 {
			args = append(args, "-s", strconv.Itoa(size))
		}
	}
	args = append(args, host)

	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond+3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cctx, "ping", args...).Output()
	if err != nil {
		return false, nil // non-zero exit / timeout → unreachable
	}
	if m := timeRe.FindSubmatch(out); m != nil {
		if v, perr := strconv.ParseFloat(string(m[1]), 64); perr == nil {
			return true, &v
		}
	}
	return true, nil // reachable but latency not parsed
}

type pingResult struct {
	ok bool
	ms *float64
}

// ---------------------------------------------------------------------------
// WebSocket hub — one writer goroutine per client, buffered fan-out
// ---------------------------------------------------------------------------

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // match the Python server's openness
}

type client struct {
	conn *websocket.Conn
	send chan []byte
}

type hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

func newHub() *hub { return &hub{clients: make(map[*client]struct{})} }

func (h *hub) register(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) unregister(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

func (h *hub) broadcast(v any) {
	msg, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default: // slow client — drop this frame rather than block the loop
		}
	}
}

var H = newHub()

// ---------------------------------------------------------------------------
// Background ping loop
// ---------------------------------------------------------------------------

func pingLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		t0 := time.Now()
		s := getSettings()

		if s.Paused == 1 {
			sleepCtx(ctx, 250*time.Millisecond)
			continue
		}

		hosts, err := hostAddrs(ctx)
		if err == nil && len(hosts) > 0 {
			ts := time.Now().Unix()
			results := make([]pingResult, len(hosts))
			var wg sync.WaitGroup
			for i, host := range hosts {
				wg.Add(1)
				go func(i int, host string) {
					defer wg.Done()
					ok, ms := pingOnce(ctx, host, s.PacketSize, s.TimeoutMs)
					results[i] = pingResult{ok, ms}
				}(i, host)
			}
			wg.Wait()

			if err := insertPings(ctx, ts, hosts, results); err != nil {
				log.Printf("insert pings: %v", err)
			}

			data := make(map[string]any, len(hosts))
			for i, host := range hosts {
				data[host] = map[string]any{"ok": results[i].ok, "ms": results[i].ms}
			}
			H.broadcast(map[string]any{"type": "ping", "ts": ts, "data": data})
		}

		sleep := time.Duration(s.Interval)*time.Second - time.Since(t0)
		if sleep < 100*time.Millisecond {
			sleep = 100 * time.Millisecond
		}
		sleepCtx(ctx, sleep)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func insertPings(ctx context.Context, ts int64, hosts []string, results []pingResult) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO pings (host,timestamp,success,latency_ms) VALUES (?,?,?,?)")
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for i, host := range hosts {
		ok := 0
		if results[i].ok {
			ok = 1
		}
		var ms any
		if results[i].ms != nil {
			ms = *results[i].ms
		}
		if _, err := stmt.ExecContext(ctx, host, ts, ok, ms); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Data access
// ---------------------------------------------------------------------------

type hostRow struct {
	Host  string `json:"host"`
	Label string `json:"label"`
}

func listHosts(ctx context.Context) ([]hostRow, error) {
	rows, err := db.QueryContext(ctx, "SELECT host,label FROM hosts ORDER BY added_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []hostRow{}
	for rows.Next() {
		var h hostRow
		if err := rows.Scan(&h.Host, &h.Label); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func hostAddrs(ctx context.Context) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT host FROM hosts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func queryRange(r *http.Request) (start, end int64) {
	now := time.Now().Unix()
	end = now
	start = now - 3600
	if v := r.URL.Query().Get("end"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			end = n
		}
	}
	if v := r.URL.Query().Get("start"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			start = n
		}
	}
	return start, end
}

// ---------------------------------------------------------------------------
// REST API — hosts
// ---------------------------------------------------------------------------

func handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := listHosts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, hosts)
}

func handleAddHost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host  string  `json:"host"`
		Label *string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Invalid host"})
		return
	}
	host := strings.TrimSpace(body.Host)
	// Reject empty hosts and values starting with "-": host is passed as an
	// argument to the system `ping`, and a leading dash would be parsed as a
	// flag (argument injection). Valid hostnames/IPs never begin with "-".
	if host == "" || strings.HasPrefix(host, "-") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Invalid host"})
		return
	}
	label := host
	if body.Label != nil && *body.Label != "" {
		label = *body.Label
	}
	_, err := db.ExecContext(r.Context(),
		"INSERT INTO hosts (host,label,added_at) VALUES (?,?,?)",
		host, label, time.Now().Unix())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Host already exists"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"host": host, "label": label})
}

func handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("host")
	if _, err := db.ExecContext(r.Context(), "DELETE FROM hosts WHERE host=?", host); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// REST API — settings
// ---------------------------------------------------------------------------

func handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, getSettings())
}

func handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "Invalid body"})
		return
	}
	settingsMu.Lock()
	changed := in.applyUpdate(&settings)
	current := settings
	settingsMu.Unlock()

	if changed {
		if err := persistSettings(r.Context()); err != nil {
			log.Printf("persist settings: %v", err)
		}
		H.broadcast(map[string]any{"type": "settings", "settings": current})
	}
	writeJSON(w, http.StatusOK, current)
}

// ---------------------------------------------------------------------------
// REST API — history & drops
// ---------------------------------------------------------------------------

type pingRecord struct {
	Host string   `json:"host"`
	Ts   int64    `json:"ts"`
	OK   bool     `json:"ok"`
	Ms   *float64 `json:"ms"`
}

func queryHistory(ctx context.Context, start, end int64, host string) ([]pingRecord, error) {
	q := "SELECT host,timestamp,success,latency_ms FROM pings WHERE timestamp BETWEEN ? AND ?"
	args := []any{start, end}
	if host != "" {
		q += " AND host=?"
		args = append(args, host)
	}
	q += " ORDER BY timestamp"

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []pingRecord{}
	for rows.Next() {
		var rec pingRecord
		var success int
		var ms sql.NullFloat64
		if err := rows.Scan(&rec.Host, &rec.Ts, &success, &ms); err != nil {
			return nil, err
		}
		rec.OK = success != 0
		if ms.Valid {
			v := ms.Float64
			rec.Ms = &v
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	start, end := queryRange(r)
	recs, err := queryHistory(r.Context(), start, end, r.URL.Query().Get("host"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

type dropEvent struct {
	Ts      int64    `json:"ts"`
	Failed  []string `json:"failed"`
	Total   int      `json:"total"`
	Polled  int      `json:"polled"`
	AllDown bool     `json:"all_down"`
}

// computeDrops groups statuses by timestamp and emits one event per round that
// had at least one failed host. totalHosts is the current host count.
func computeDrops(ctx context.Context, start, end *int64) ([]dropEvent, error) {
	totalHosts, err := func() (int, error) {
		var n int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM hosts").Scan(&n)
		return n, err
	}()
	if err != nil {
		return nil, err
	}

	q := "SELECT timestamp,host,success FROM pings"
	var args []any
	if start != nil && end != nil {
		q += " WHERE timestamp BETWEEN ? AND ?"
		args = append(args, *start, *end)
	}
	q += " ORDER BY timestamp"

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type status struct {
		host string
		ok   bool
	}
	byTs := map[int64][]status{}
	var order []int64
	for rows.Next() {
		var ts int64
		var host string
		var ok int
		if err := rows.Scan(&ts, &host, &ok); err != nil {
			return nil, err
		}
		if _, seen := byTs[ts]; !seen {
			order = append(order, ts)
		}
		byTs[ts] = append(byTs[ts], status{host, ok != 0})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })

	events := []dropEvent{}
	for _, ts := range order {
		statuses := byTs[ts]
		var failed []string
		for _, s := range statuses {
			if !s.ok {
				failed = append(failed, s.host)
			}
		}
		if len(failed) > 0 {
			events = append(events, dropEvent{
				Ts:      ts,
				Failed:  failed,
				Total:   totalHosts,
				Polled:  len(statuses),
				AllDown: len(failed) == len(statuses) && len(statuses) > 0,
			})
		}
	}
	return events, nil
}

func handleDrops(w http.ResponseWriter, r *http.Request) {
	start, end := queryRange(r)
	events, err := computeDrops(r.Context(), &start, &end)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// ---------------------------------------------------------------------------
// CSV export
// ---------------------------------------------------------------------------

// hasRange reports whether either start or end query param was supplied.
func hasRange(r *http.Request) bool {
	return r.URL.Query().Get("start") != "" || r.URL.Query().Get("end") != ""
}

func csvHeaders(w http.ResponseWriter, prefix string) *csv.Writer {
	fname := fmt.Sprintf("pingchecker_%s_%s.csv", prefix, time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fname))
	w.WriteHeader(http.StatusOK)
	return csv.NewWriter(w)
}

func fmtLatency(ms *float64) string {
	if ms == nil {
		return ""
	}
	rounded := float64(int64(*ms*1000+0.5)) / 1000 // round to 3 decimals
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

func handleExportHistory(w http.ResponseWriter, r *http.Request) {
	labels := map[string]string{}
	if hosts, err := listHosts(r.Context()); err == nil {
		for _, h := range hosts {
			labels[h.Host] = h.Label
		}
	}

	var recs []pingRecord
	var err error
	if hasRange(r) {
		start, end := queryRange(r)
		recs, err = queryHistory(r.Context(), start, end, "")
	} else {
		recs, err = queryHistory(r.Context(), 0, time.Now().Unix(), "")
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	// match the Python ORDER BY timestamp,host
	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].Ts != recs[j].Ts {
			return recs[i].Ts < recs[j].Ts
		}
		return recs[i].Host < recs[j].Host
	})

	cw := csvHeaders(w, "history")
	defer cw.Flush()
	cw.Write([]string{"timestamp", "datetime", "host", "label", "success", "latency_ms"})
	for _, rec := range recs {
		label := labels[rec.Host]
		if label == "" {
			label = rec.Host
		}
		success := "false"
		if rec.OK {
			success = "true"
		}
		cw.Write([]string{
			strconv.FormatInt(rec.Ts, 10),
			time.Unix(rec.Ts, 0).Format("2006-01-02 15:04:05"),
			rec.Host, label, success, fmtLatency(rec.Ms),
		})
	}
}

func handleExportDrops(w http.ResponseWriter, r *http.Request) {
	var events []dropEvent
	var err error
	if hasRange(r) {
		start, end := queryRange(r)
		events, err = computeDrops(r.Context(), &start, &end)
	} else {
		events, err = computeDrops(r.Context(), nil, nil)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}

	cw := csvHeaders(w, "drops")
	defer cw.Flush()
	cw.Write([]string{"timestamp", "datetime", "severity", "failed_hosts", "hosts_down", "total_hosts"})
	for _, d := range events {
		sev := "PARTIAL"
		if d.AllDown {
			sev = "TOTAL"
		}
		cw.Write([]string{
			strconv.FormatInt(d.Ts, 10),
			time.Unix(d.Ts, 0).Format("2006-01-02 15:04:05"),
			sev,
			strings.Join(d.Failed, "; "),
			strconv.Itoa(len(d.Failed)),
			strconv.Itoa(d.Total),
		})
	}
}

// ---------------------------------------------------------------------------
// WebSocket endpoint
// ---------------------------------------------------------------------------

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 64)}
	H.register(c)

	// Writer goroutine — the only place that writes to conn.
	go func() {
		defer conn.Close()
		for msg := range c.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// Initial snapshot: host list + current settings.
	if hosts, err := listHosts(r.Context()); err == nil {
		if msg, err := json.Marshal(map[string]any{"type": "hosts", "hosts": hosts}); err == nil {
			c.send <- msg
		}
	}
	if msg, err := json.Marshal(map[string]any{"type": "settings", "settings": getSettings()}); err == nil {
		c.send <- msg
	}

	// Read loop — keeps the connection open and detects disconnect.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	H.unregister(c)
}

// ---------------------------------------------------------------------------
// Static assets
// ---------------------------------------------------------------------------

func handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// ---------------------------------------------------------------------------
// Browser auto-open
// ---------------------------------------------------------------------------

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd, args = "open", []string{url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	dir := dataDir()
	dbPath = filepath.Join(dir, "pings.db")
	configPath = filepath.Join(dir, "config.json")

	if err := openDB(); err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := initDB(ctx); err != nil {
		log.Fatalf("init db: %v", err)
	}
	if err := seedHosts(ctx); err != nil {
		log.Printf("seed hosts: %v", err)
	}
	if err := loadSettings(ctx); err != nil {
		log.Printf("load settings: %v", err)
	}

	sub, _ := fs.Sub(staticFS, "static")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/hosts", handleListHosts)
	mux.HandleFunc("POST /api/hosts", handleAddHost)
	mux.HandleFunc("DELETE /api/hosts/{host}", handleDeleteHost)
	mux.HandleFunc("GET /api/settings", handleGetSettings)
	mux.HandleFunc("PUT /api/settings", handlePutSettings)
	mux.HandleFunc("GET /api/history", handleHistory)
	mux.HandleFunc("GET /api/drops", handleDrops)
	mux.HandleFunc("GET /api/export/history.csv", handleExportHistory)
	mux.HandleFunc("GET /api/export/drops.csv", handleExportDrops)
	mux.HandleFunc("/ws", handleWS)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("GET /{$}", handleIndex)

	srv := &http.Server{Addr: addr, Handler: mux}

	go pingLoop(ctx)

	// Listen explicitly so we can start the loop/browser only once bound.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen on %s: %v (is another instance running?)", addr, err)
	}

	fmt.Printf("\n  PingChecker %s running → http://localhost:8765\n\n", version)
	go func() {
		time.Sleep(800 * time.Millisecond)
		openBrowser("http://localhost:8765")
	}()

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}
