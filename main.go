package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/Ghostpanter/clash-exporter/collector"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var version = "dev"

var (
	collectDest    bool
	collectTracing bool
	port           int
	listenAddr     string
)

func init() {
	flag.BoolVar(&collectDest, "collectDest", false, "label traffic by destination host.\nWarning: this creates one series per destination and can overwhelm Prometheus. Leave off on a router.")
	flag.BoolVar(&collectTracing, "collectTracing", false, "collect /profile/tracing.\nCurrent Mihomo builds used by OpenClash no longer expose this endpoint; the collector stops itself when the core returns 404.")
	flag.IntVar(&port, "port", 2112, "port to listen on. Ignored when -listen is set")
	flag.StringVar(&listenAddr, "listen", "", "address to listen on, for example 0.0.0.0:2112. Overrides -port")
}

func getEnvOrDefault(key string, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}

func main() {
	flag.Parse()

	addr := listenAddr
	if addr == "" {
		addr = fmt.Sprintf(":%d", port)
	}

	config := collector.CollectConfig{
		CollectDest:    collectDest,
		CollectTracing: collectTracing,
		ClashHost:      getEnvOrDefault("CLASH_HOST", "127.0.0.1:9090"),
		ClashToken:     getEnvOrDefault("CLASH_TOKEN", ""),
	}
	http.Handle("/metrics", promhttp.Handler())
	log.Printf("clash-exporter %s listening on %s, controller %s, collectDest=%v collectTracing=%v", version, addr, config.ClashHost, collectDest, collectTracing)

	go collector.Start(config)

	log.Fatal(http.ListenAndServe(addr, nil))
}
