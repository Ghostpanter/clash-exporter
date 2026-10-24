package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Info struct{}

var (
	clashInfo *prometheus.GaugeVec
	clashUp   prometheus.Gauge
)

func (*Info) Name() string { return "info" }

type versionMessage struct {
	Version string `json:"version"`
	Premium bool   `json:"premium"`
	Meta    bool   `json:"meta"`
}

func (*Info) Collect(config CollectConfig) error {
	log.Println("starting collector:", "info")
	for {
		if err := scrapeVersion(config); err != nil {
			clashInfo.Reset()
			clashUp.Set(0)
			return err
		}
		time.Sleep(30 * time.Second)
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
	clashInfo.Reset()
	clashInfo.WithLabelValues(result.Version, fmt.Sprintf("%t", premium)).Set(1)
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
