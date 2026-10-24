package collector

import (
	"context"
	"fmt"
	"log"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/prometheus/client_golang/prometheus"
)

type memoryMessage struct {
	Inuse   int64 `json:"inuse"`
	OSLimit int64 `json:"oslimit"`
}

var clashMemory *prometheus.GaugeVec

type Memory struct{}

func (*Memory) Name() string { return "memory" }

func (*Memory) Collect(config CollectConfig) error {
	log.Println("starting collector:", "memory")
	ctx := context.Background()
	conn, _, err := dialWS(ctx, config, "/memory")
	if err != nil {
		return fmt.Errorf("dial /memory: %w", err)
	}
	defer conn.Close(websocket.StatusInternalError, "reconnect")
	defer clashMemory.Reset()

	// Mihomo writes a fake 0 as the first sample so charts start at the origin. Skip it.
	first := true
	for {
		var m memoryMessage
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			return fmt.Errorf("read /memory: %w", err)
		}
		if first {
			first = false
			// Mihomo forces the first sample to 0 so its own charts start at the
			// origin, even when the process is already using memory. A core that
			// does not do that (non-zero first sample) is recorded immediately.
			if m.Inuse == 0 && m.OSLimit == 0 {
				continue
			}
		}
		clashMemory.WithLabelValues("inuse").Set(float64(m.Inuse))
		clashMemory.WithLabelValues("oslimit").Set(float64(m.OSLimit))
	}
}

func init() {
	clashMemory = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "clash",
		Name:      "memory_bytes",
		Help:      "Mihomo memory usage reported by the external controller",
	}, []string{"type"})
	prometheus.MustRegister(clashMemory)
	Register(new(Memory))
}
