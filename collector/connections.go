package collector

import (
	"context"
	"fmt"
	"log"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/prometheus/client_golang/prometheus"
)

type connectionMessage struct {
	DownloadTotal int64         `json:"downloadTotal"`
	UploadTotal   int64         `json:"uploadTotal"`
	Connections   []Connections `json:"connections"`
}

type Metadata struct {
	Network         string `json:"network"`
	Type            string `json:"type"`
	SourceIP        string `json:"sourceIP"`
	DestinationIP   string `json:"destinationIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	SniffHost       string `json:"sniffHost"`
	DNSMode         string `json:"dnsMode"`
	ProcessPath     string `json:"processPath"`
	SpecialProxy    string `json:"specialProxy"`
}

type Connections struct {
	ID          string   `json:"id"`
	Metadata    Metadata `json:"metadata"`
	Upload      int64    `json:"upload"`
	Download    int64    `json:"download"`
	Chains      []string `json:"chains"`
	Rule        string   `json:"rule"`
	RulePayload string   `json:"rulePayload"`
}

type connBytes struct {
	Upload   int64
	Download int64
}

var (
	uploadTotal         prometheus.Gauge
	downloadTotal       prometheus.Gauge
	activeConnections   prometheus.Gauge
	networkTrafficTotal *prometheus.CounterVec
)

type Connection struct {
	connectionCache map[string]connBytes
}

func (c *Connection) Name() string { return "connections" }

func (c *Connection) Collect(config CollectConfig) error {
	log.Println("starting collector:", c.Name())
	ctx := context.Background()
	conn, _, err := dialWS(ctx, config, "/connections")
	if err != nil {
		return fmt.Errorf("dial /connections: %w", err)
	}
	defer conn.Close(websocket.StatusInternalError, "reconnect")
	// Don't keep reporting the last snapshot while the core is down.
	defer activeConnections.Set(0)

	baseline := true
	for {
		var m connectionMessage
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			return fmt.Errorf("read /connections: %w", err)
		}
		uploadTotal.Set(float64(m.UploadTotal))
		downloadTotal.Set(float64(m.DownloadTotal))
		activeConnections.Set(float64(len(m.Connections)))

		alive := make(map[string]struct{}, len(m.Connections))
		// The first snapshot after each (re)connect is a baseline. Bytes already
		// on those connections are not new traffic; counting them makes Grafana
		// draw a multi-gigabyte spike every time the exporter or the core restarts.
		// Connections that show up later are counted from the first time we see them,
		// so a short flow that appears in only one snapshot is not dropped.
		// Connections we were already tracking keep their delta, including bytes
		// transferred while the websocket was down.
		for _, connection := range m.Connections {
			prev, seen := c.connectionCache[connection.ID]
			policy := policyOf(connection.Chains)
			destination := destinationOf(connection.Metadata, config.CollectDest)
			if !(baseline && !seen) {
				networkTrafficTotal.WithLabelValues(connection.Metadata.SourceIP, destination, policy, "download").
					Add(nonNegativeDelta(connection.Download, prev.Download))
				networkTrafficTotal.WithLabelValues(connection.Metadata.SourceIP, destination, policy, "upload").
					Add(nonNegativeDelta(connection.Upload, prev.Upload))
			}
			c.connectionCache[connection.ID] = connBytes{Upload: connection.Upload, Download: connection.Download}
			alive[connection.ID] = struct{}{}
		}
		for id := range c.connectionCache {
			if _, ok := alive[id]; !ok {
				delete(c.connectionCache, id)
			}
		}
		baseline = false
	}
}

func init() {
	uploadTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "clash",
		Name:      "upload_bytes_total",
		Help:      "Total upload bytes reported by the core",
	})
	downloadTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "clash",
		Name:      "download_bytes_total",
		Help:      "Total download bytes reported by the core",
	})
	activeConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "clash",
		Name:      "active_connections",
		Help:      "Active connections",
	})
	networkTrafficTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "clash",
		Name:      "network_traffic_bytes_total",
		Help:      "Bytes downloaded or uploaded, by source, destination, and selected proxy.",
	}, []string{"source", "destination", "policy", "type"})

	prometheus.MustRegister(uploadTotal, downloadTotal, activeConnections, networkTrafficTotal)
	Register(&Connection{connectionCache: map[string]connBytes{}})
}
