package collector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	dnsReqDuration    *prometheus.HistogramVec
	ruleMatchDuration prometheus.Histogram
	proxyDialDuration *prometheus.HistogramVec
)

type tracingMessage struct {
	Duration int      `json:"duration"`
	Metadata Metadata `json:"metadata"`
	Payload  string   `json:"payload"`
	Proxy    string   `json:"proxy"`
	Rule     string   `json:"rule"`
	Type     string   `json:"type"`
	DnsType  string   `json:"dnsType"`
}

type Tracing struct{}

func (*Tracing) Name() string { return "tracing" }

func (*Tracing) Collect(config CollectConfig) error {
	if !config.CollectTracing {
		return nil
	}
	log.Println("starting collector:", "tracing")
	ctx := context.Background()
	conn, resp, err := dialWS(ctx, config, "/profile/tracing")
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest) {
			log.Println("collector: tracing stopped: this Mihomo/OpenClash core does not expose /profile/tracing")
			return nil
		}
		return fmt.Errorf("dial /profile/tracing: %w", err)
	}
	defer conn.Close(websocket.StatusInternalError, "reconnect")

	for {
		var m tracingMessage
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			return fmt.Errorf("read /profile/tracing: %w", err)
		}
		switch m.Type {
		case "RuleMatch":
			ruleMatchDuration.Observe(float64(m.Duration) / 1000)
		case "DNSRequest":
			dnsReqDuration.WithLabelValues(m.DnsType).Observe(float64(m.Duration) / 1000)
		case "ProxyDial":
			proxyDialDuration.WithLabelValues(m.Proxy).Observe(float64(m.Duration) / 1000)
		}
	}
}

func init() {
	dnsReqDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "clash",
		Subsystem: "tracing",
		Name:      "dns_request_duration_milliseconds",
		Help:      "DNS request duration in milliseconds",
		Buckets:   []float64{1, 5, 10, 25, 50, 75, 100, 250, 500},
	}, []string{"type"})
	ruleMatchDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "clash",
		Subsystem: "tracing",
		Name:      "rule_match_duration_milliseconds",
		Help:      "Rule match duration in milliseconds",
		Buckets:   []float64{1, 5, 10, 25, 50, 75, 100, 250, 500},
	})
	proxyDialDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "clash",
		Subsystem: "tracing",
		Name:      "proxy_dial_duration_milliseconds",
		Help:      "Proxy dial duration in milliseconds",
		Buckets:   []float64{1, 10, 50, 100, 250, 500, 1000, 2500, 5000},
	}, []string{"policy"})
	prometheus.MustRegister(dnsReqDuration, ruleMatchDuration, proxyDialDuration)
	Register(new(Tracing))
}
