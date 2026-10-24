package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// mihomoWorld speaks the subset of the external controller OpenClash actually exposes:
// Bearer auth, newline-terminated JSON websocket frames, a fake zero memory sample,
// and 404 for /profile/tracing.
type mihomoWorld struct {
	mu        sync.Mutex
	token     string
	reject    bool
	version   string
	meta      bool
	mode      string
	conns     []Connections
	downTotal int64
	upTotal   int64
	mem       int64
	fakeZero  bool
	live      []*websocket.Conn
	// memHold pauses the memory websocket after its first frame so a test can
	// observe that Mihomo's fake 0 was not published.
	memHold bool
	memHeld chan struct{}
	memCont chan struct{}
}

func (w *mihomoWorld) snapshot() (int64, int64, []Connections) {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := append([]Connections(nil), w.conns...)
	return w.downTotal, w.upTotal, out
}

func (w *mihomoWorld) authorize(r *http.Request) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.reject {
		return false
	}
	if w.token == "" {
		return true
	}
	header := r.Header.Get("Authorization")
	bearer, token, found := strings.Cut(header, " ")
	return found && bearer == "Bearer" && token == w.token
}

func (w *mihomoWorld) addLive(c *websocket.Conn) {
	w.mu.Lock()
	w.live = append(w.live, c)
	w.mu.Unlock()
}

func (w *mihomoWorld) forget(c *websocket.Conn) {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.live[:0]
	for _, x := range w.live {
		if x != c {
			out = append(out, x)
		}
	}
	w.live = out
}

// closeLive drops already-upgraded sockets. Mihomo stops pushing when the core
// goes away; flipping the secret alone does not, because auth is only checked
// on the handshake.
func (w *mihomoWorld) closeLive() {
	w.mu.Lock()
	live := w.live
	w.live = nil
	w.mu.Unlock()
	for _, c := range live {
		_ = c.Close(websocket.StatusGoingAway, "core down")
	}
}

func (w *mihomoWorld) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(rw http.ResponseWriter, r *http.Request) {
		if !w.authorize(r) {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.mu.Lock()
		body := fmt.Sprintf(`{"meta":%t,"version":%q}`, w.meta, w.version)
		w.mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(rw, body)
	})
	mux.HandleFunc("/configs", func(rw http.ResponseWriter, r *http.Request) {
		if !w.authorize(r) {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.mu.Lock()
		mode := w.mode
		w.mu.Unlock()
		// Extra fields mirror GetGeneral: unknown keys must be ignored, mode is a string.
		fmt.Fprintf(rw, `{"port":7890,"socks-port":7891,"mode":%q,"log-level":"info","ipv6":false}`, mode)
	})
	mux.HandleFunc("/profile/tracing", func(rw http.ResponseWriter, r *http.Request) {
		http.NotFound(rw, r)
	})
	mux.HandleFunc("/connections", func(rw http.ResponseWriter, r *http.Request) {
		if !w.authorize(r) {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(rw, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		w.addLive(c)
		defer w.forget(c)
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			down, up, conns := w.snapshot()
			payload := map[string]any{
				"downloadTotal": down,
				"uploadTotal":   up,
				"connections":   conns,
			}
			var buf strings.Builder
			enc := json.NewEncoder(&buf) // trailing newline, same as mihomo's json.Encoder
			if err := enc.Encode(payload); err != nil {
				return
			}
			if err := c.Write(ctx, websocket.MessageText, []byte(buf.String())); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	})
	mux.HandleFunc("/memory", func(rw http.ResponseWriter, r *http.Request) {
		if !w.authorize(r) {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(rw, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		w.addLive(c)
		defer w.forget(c)
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		first := true
		paused := false
		for {
			w.mu.Lock()
			val := w.mem
			fake := w.fakeZero
			hold := w.memHold && !paused
			if hold {
				w.memHold = false
			}
			w.mu.Unlock()
			if first && fake {
				val = 0
			}
			first = false
			var buf strings.Builder
			_ = json.NewEncoder(&buf).Encode(map[string]int64{"inuse": val, "oslimit": 0})
			if err := c.Write(ctx, websocket.MessageText, []byte(buf.String())); err != nil {
				return
			}
			if hold {
				paused = true
				select {
				case <-ctx.Done():
					return
				case w.memHeld <- struct{}{}:
				}
				select {
				case <-ctx.Done():
					return
				case <-w.memCont:
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	})
	return mux
}

func samples() map[string]float64 {
	out := map[string]float64{}
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return out
	}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			var b strings.Builder
			b.WriteString(mf.GetName())
			for _, lp := range m.GetLabel() {
				fmt.Fprintf(&b, "{%s=%s}", lp.GetName(), lp.GetValue())
			}
			var v float64
			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				v = m.GetCounter().GetValue()
			case dto.MetricType_GAUGE:
				v = m.GetGauge().GetValue()
			default:
				continue
			}
			out[b.String()] = v
		}
	}
	return out
}

func waitUntil(t *testing.T, d time.Duration, ok func(map[string]float64) bool) map[string]float64 {
	t.Helper()
	deadline := time.Now().Add(d)
	var last map[string]float64
	for time.Now().Before(deadline) {
		last = samples()
		if ok(last) {
			return last
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("condition not met, samples:\n%v", last)
	return nil
}

func metric(s map[string]float64, name string) (float64, bool) {
	v, ok := s[name]
	return v, ok
}

func TestDialDoesNotHang(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			time.Sleep(30 * time.Second)
			c.Close()
		}
	}()
	old := dialTimeout
	dialTimeout = 200 * time.Millisecond
	t.Cleanup(func() { dialTimeout = old })

	start := time.Now()
	_, _, err = dialWS(context.Background(), CollectConfig{ClashHost: ln.Addr().String()}, "/connections")
	if err == nil {
		t.Fatal("expected dial error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("dial hung for %s", time.Since(start))
	}
}

func TestSimulateMihomo(t *testing.T) {
	retryStep = 30 * time.Millisecond
	infoInterval = 20 * time.Millisecond
	modeInterval = 20 * time.Millisecond
	t.Cleanup(func() {
		retryStep = 10 * time.Second
		infoInterval = 30 * time.Second
		modeInterval = 15 * time.Second
	})

	world := &mihomoWorld{
		token:     "p@ss word/+=",
		version:   "v1.19.15",
		meta:      true,
		mode:      "rule",
		fakeZero:  true,
		mem:       4096,
		memHold:   true,
		memHeld:   make(chan struct{}, 1),
		memCont:   make(chan struct{}),
		downTotal: 3_000_000_000,
		upTotal:   2_000_000_000,
		conns: []Connections{{
			ID: "alive",
			Metadata: Metadata{
				Network:       "tcp",
				SourceIP:      "10.0.0.8",
				DestinationIP: "1.1.1.1",
				Host:          "1.1.1.1",
				SniffHost:     "example.com",
			},
			Upload:   100,
			Download: 5_000_000_000,
			Chains:   []string{"香港 01", "手动选择"},
		}},
	}
	srv := &http.Server{Handler: world.handler()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close() })

	// Wrong secret must not succeed and must not panic.
	if err := scrapeVersion(CollectConfig{ClashHost: ln.Addr().String(), ClashToken: "nope"}); err == nil {
		t.Fatal("expected unauthorized /version")
	}

	trErr := make(chan error, 1)
	go func() {
		trErr <- (&Tracing{}).Collect(CollectConfig{
			CollectTracing: true,
			ClashHost:      ln.Addr().String(),
			ClashToken:     world.token,
		})
	}()
	select {
	case err := <-trErr:
		if err != nil {
			t.Fatalf("tracing 404 should stop the collector, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tracing collector did not return on 404")
	}

	Start(CollectConfig{
		CollectDest:    true,
		CollectTracing: true,
		ClashHost:      ln.Addr().String(),
		ClashToken:     world.token,
	})

	// Gathered labels are alphabetical. The fake 0 is the only memory frame
	// until memCont is closed; publishing it would stick the gauge at 0.
	select {
	case <-world.memHeld:
	case <-time.After(2 * time.Second):
		t.Fatal("memory websocket did not send its first sample")
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if v, ok := samples()["clash_memory_bytes{type=inuse}"]; ok {
			t.Fatalf("fake zero memory sample was published: %v", v)
		}
		time.Sleep(15 * time.Millisecond)
	}
	close(world.memCont)

	// Prometheus gathers labels in alphabetical order, not declaration order.
	infoKey := func(version string) string {
		return "clash_info{premium=true}{version=" + version + "}"
	}
	trafficKey := func(source, destination, policy, typ string) string {
		return "clash_network_traffic_bytes_total{destination=" + destination + "}{policy=" + policy + "}{source=" + source + "}{type=" + typ + "}"
	}
	key := trafficKey("10.0.0.8", "example.com", "香港 01", "download")
	uploadKey := trafficKey("10.0.0.8", "example.com", "香港 01", "upload")
	shortKey := trafficKey("10.0.0.9", "", "unknown", "download")

	s := waitUntil(t, 3*time.Second, func(s map[string]float64) bool {
		up, ok := metric(s, "clash_up")
		info, ok2 := metric(s, infoKey("v1.19.15"))
		mode, ok3 := metric(s, "clash_mode{mode=rule}")
		mem, ok4 := metric(s, "clash_memory_bytes{type=inuse}")
		down, ok5 := metric(s, "clash_download_bytes_total")
		active, ok6 := metric(s, "clash_active_connections")
		upBytes, ok7 := metric(s, "clash_upload_bytes_total")
		return ok && up == 1 && ok2 && info == 1 && ok3 && mode == 1 && ok4 && mem == 4096 && ok5 && down == 3_000_000_000 && ok6 && active == 1 && ok7 && upBytes == 2_000_000_000
	})

	// Bytes already sitting on a live connection when we attach must not become a counter spike.
	if v := s[key]; v != 0 {
		t.Fatalf("historical download counted as new traffic: %v", v)
	}
	if v, ok := s[uploadKey]; ok && v != 0 {
		t.Fatalf("historical upload counted as new traffic: %v", v)
	}

	world.mu.Lock()
	world.conns[0].Download = 5_000_000_300
	world.conns = append(world.conns, Connections{
		ID:       "short",
		Metadata: Metadata{SourceIP: "10.0.0.9"},
		Download: 40,
		Chains:   nil,
	})
	world.mu.Unlock()

	s = waitUntil(t, 3*time.Second, func(s map[string]float64) bool {
		return s[key] == 300 && s[shortKey] == 40
	})

	// Same snapshot again must not double-count. Give the pusher a few intervals.
	time.Sleep(80 * time.Millisecond)
	if samples()[key] != 300 {
		t.Fatalf("counter moved without new bytes: %v", samples()[key])
	}

	// Same websocket, new connection id: bytes on a connection we have never tracked
	// are counted from the first snapshot. Only the first frame of a session is a baseline.
	world.mu.Lock()
	world.conns = []Connections{{
		ID:       "after-reload",
		Metadata: Metadata{SourceIP: "10.0.0.8", SniffHost: "example.com"},
		Download: 9_000,
		Chains:   []string{"香港 01"},
	}}
	world.downTotal = 10
	world.mu.Unlock()
	s = waitUntil(t, 3*time.Second, func(s map[string]float64) bool {
		return s["clash_active_connections"] == 1 && s["clash_download_bytes_total"] == 10 && s[key] == 9_300
	})

	world.mu.Lock()
	world.conns[0].Download = 9_050
	world.mu.Unlock()
	waitUntil(t, 3*time.Second, func(s map[string]float64) bool {
		return s[key] == 9_350
	})

	// Counter reset on the same connection id must not go backwards, and later growth still counts.
	world.mu.Lock()
	world.conns[0].Download = 10
	world.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	if samples()[key] != 9_350 {
		t.Fatalf("counter decreased: %v", samples()[key])
	}
	world.mu.Lock()
	world.conns[0].Download = 25
	world.mu.Unlock()
	waitUntil(t, 3*time.Second, func(s map[string]float64) bool { return s[key] == 9_365 })

	// A real zero after the skipped fake sample must be recorded.
	world.mu.Lock()
	world.mem = 0
	world.mu.Unlock()
	waitUntil(t, 3*time.Second, func(s map[string]float64) bool {
		v, ok := metric(s, "clash_memory_bytes{type=inuse}")
		return ok && v == 0
	})

	world.mu.Lock()
	world.fakeZero = false
	world.mem = 77
	world.reject = true
	world.mu.Unlock()
	world.closeLive()
	waitUntil(t, 3*time.Second, func(s map[string]float64) bool {
		v, ok := metric(s, "clash_up")
		_, info := metric(s, infoKey("v1.19.15"))
		_, mem := metric(s, "clash_memory_bytes{type=inuse}")
		_, modeOn := metric(s, "clash_mode{mode=rule}")
		active, _ := metric(s, "clash_active_connections")
		return ok && v == 0 && !info && !mem && !modeOn && active == 0
	})

	// New websocket session after the core comes back. This id was never tracked,
	// so the first frame is a baseline and 8GB must not enter the counter.
	// Swap the snapshot only after the old socket is gone, or that session would count it.
	world.mu.Lock()
	world.conns = []Connections{{
		ID:       "post-restart",
		Metadata: Metadata{SourceIP: "10.0.0.8", SniffHost: "example.com"},
		Download: 8_000_000_000,
		Chains:   []string{"香港 01"},
	}}
	world.downTotal = 8_000_000_000
	world.reject = false
	world.version = "v1.19.16"
	world.mode = "global"
	world.mu.Unlock()
	waitUntil(t, 3*time.Second, func(s map[string]float64) bool {
		up, _ := metric(s, "clash_up")
		info, okInfo := metric(s, infoKey("v1.19.16"))
		_, old := metric(s, infoKey("v1.19.15"))
		mode, okMode := metric(s, "clash_mode{mode=global}")
		_, oldMode := metric(s, "clash_mode{mode=rule}")
		mem, okMem := metric(s, "clash_memory_bytes{type=inuse}")
		down, _ := metric(s, "clash_download_bytes_total")
		return up == 1 && okInfo && info == 1 && !old && okMode && mode == 1 && !oldMode && okMem && mem == 77 && down == 8_000_000_000 && s[key] == 9_365
	})

	world.mu.Lock()
	world.conns[0].Download = 8_000_000_010
	world.mu.Unlock()
	waitUntil(t, 3*time.Second, func(s map[string]float64) bool {
		return s[key] == 9_375
	})
}
