package collector

import (
	"log"
	"time"
)

type Collector interface {
	Name() string
	Collect(config CollectConfig) error
}

type CollectConfig struct {
	CollectDest    bool
	CollectTracing bool
	ClashHost      string
	ClashToken     string
}

var collectors []Collector

func Register(c Collector) {
	collectors = append(collectors, c)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Start(config CollectConfig) {
	for _, c := range collectors {
		go func(c Collector) {
			retryCount := 0
			for {
				started := time.Now()
				if err := c.Collect(config); err != nil {
					// A collector that ran fine for a while (e.g. until OpenClash restarted the core)
					// starts the backoff from scratch instead of waiting the maximum 60s forever.
					if time.Since(started) > time.Minute {
						retryCount = 0
					}
					retryCount++
					sleepDuration := time.Duration(min(retryCount*10, 60)) * time.Second
					log.Println("collector:", c.Name(), "failed error:", err, "retry after", sleepDuration)
					time.Sleep(sleepDuration)
				} else {
					return
				}
			}
		}(c)
	}
}
