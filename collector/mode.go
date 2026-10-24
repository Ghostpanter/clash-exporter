package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Mode struct{}

var clashMode *prometheus.GaugeVec

var modeInterval = 15 * time.Second

func (*Mode) Name() string { return "mode" }

type configMessage struct {
	Mode string `json:"mode"`
}

var (
	modeMu        sync.Mutex
	publishedMode string
)

func publishMode(mode string) {
	modeMu.Lock()
	defer modeMu.Unlock()
	if publishedMode == mode {
		clashMode.WithLabelValues(mode).Set(1)
		return
	}
	clashMode.Reset()
	clashMode.WithLabelValues(mode).Set(1)
	publishedMode = mode
}

func clearMode() {
	modeMu.Lock()
	defer modeMu.Unlock()
	clashMode.Reset()
	publishedMode = ""
}

func (*Mode) Collect(config CollectConfig) error {
	log.Println("starting collector:", "mode")
	for {
		if err := scrapeMode(config); err != nil {
			clearMode()
			return err
		}
		time.Sleep(modeInterval)
	}
}

func scrapeMode(config CollectConfig) error {
	req, err := newAPIRequest(config, "/configs")
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := checkStatus(resp); err != nil {
		return fmt.Errorf("%w: %s", err, body)
	}
	var result configMessage
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if result.Mode == "" {
		return fmt.Errorf("controller /configs has empty mode")
	}
	publishMode(result.Mode)
	return nil
}

func init() {
	clashMode = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "clash",
		Name:      "mode",
		Help:      "Current tunnel mode (rule, global, direct)",
	}, []string{"mode"})
	prometheus.MustRegister(clashMode)
	Register(new(Mode))
}
