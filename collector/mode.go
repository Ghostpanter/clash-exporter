package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Mode struct{}

var clashMode *prometheus.GaugeVec

func (*Mode) Name() string { return "mode" }

type configMessage struct {
	Mode string `json:"mode"`
}

func (*Mode) Collect(config CollectConfig) error {
	log.Println("starting collector:", "mode")
	for {
		if err := scrapeMode(config); err != nil {
			return err
		}
		time.Sleep(15 * time.Second)
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
	clashMode.Reset()
	clashMode.WithLabelValues(result.Mode).Set(1)
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
