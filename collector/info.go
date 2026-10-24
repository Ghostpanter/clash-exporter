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

type Info struct{}

var (
	clashInfo *prometheus.GaugeVec
	clashUp   prometheus.Gauge
)

var infoInterval = 30 * time.Second

func (*Info) Name() string { return "info" }

type versionMessage struct {
	Version string `json:"version"`
	Premium bool   `json:"premium"`
	Meta    bool   `json:"meta"`
}

var (
	infoMu           sync.Mutex
	publishedVersion string
	publishedPremium string
)

func publishInfo(version, premium string) {
	infoMu.Lock()
	defer infoMu.Unlock()
	if publishedVersion == version && publishedPremium == premium {
		clashInfo.WithLabelValues(version, premium).Set(1)
		return
	}
	// Reset only when the labels change, so a scrape cannot observe the series missing
	// on the steady-state poll.
	clashInfo.Reset()
	clashInfo.WithLabelValues(version, premium).Set(1)
	publishedVersion, publishedPremium = version, premium
}

func clearInfo() {
	infoMu.Lock()
	defer infoMu.Unlock()
	clashInfo.Reset()
	publishedVersion, publishedPremium = "", ""
	clashUp.Set(0)
}

func (*Info) Collect(config CollectConfig) error {
	log.Println("starting collector:", "info")
	for {
		if err := scrapeVersion(config); err != nil {
			clearInfo()
			return err
		}
		time.Sleep(infoInterval)
	}
}

func scrapeVersion(config CollectConfig) error {
	req, err := newAPIRequest(config, "/version")
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
	var result versionMessage
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	// Existing Grafana dashboards label this "premium". Mihomo reports meta=true and no premium flag.
	premium := result.Premium || result.Meta
	publishInfo(result.Version, fmt.Sprintf("%t", premium))
	clashUp.Set(1)
	return nil
}

func init() {
	clashInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "clash",
		Name:      "info",
		Help:      "Core version. The premium label is true for Clash Premium and for Mihomo (meta).",
	}, []string{"version", "premium"})
	clashUp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "clash",
		Name:      "up",
		Help:      "1 if the last /version request to the external controller succeeded, 0 otherwise",
	})
	prometheus.MustRegister(clashInfo, clashUp)
	Register(new(Info))
}
