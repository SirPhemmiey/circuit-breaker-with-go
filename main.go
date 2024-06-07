package main

import (
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sony/gobreaker"
)

var (
	requestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_count",
			Help: "Number of requests.",
		},
		[]string{"state"},
	)
	callExternalAPI func() (int, error)
)

func init() {
	prometheus.MustRegister(requestCount)
}

func defaultCallExternalAPI() (int, error) {
	resp, err := http.Get("https://example.com/api")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// exponentialBackoff returns a duration with an exponential backoff strategy
func exponentialBackoff(attempt int) time.Duration {
	min := float64(time.Second)
	max := float64(30 * time.Second)
	backoff := min * math.Pow(2, float64(attempt))
	if backoff > max {
		backoff = max
	}
	jitter := rand.Float64() * backoff
	return time.Duration(jitter)
}

func main() {
	callExternalAPI = defaultCallExternalAPI

	http.Handle("/metrics", promhttp.Handler())

	settings := gobreaker.Settings{
		Name:        "API Circuit Breaker",
		MaxRequests: 5,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Increment failure count in Prometheus
			requestCount.WithLabelValues("failure").Inc()
			return counts.ConsecutiveFailures > 3
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			fmt.Printf("Circuit Breaker %s changed from %s to %s\n", name, from, to)
			requestCount.WithLabelValues(to.String()).Inc()
		},
	}
	cb := gobreaker.NewCircuitBreaker(settings)

	http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		// _, err := cb.Execute(func() (interface{}, error) {
		// 	return callExternalAPI()
		// })
		var result interface{}
		var err error
		attempts := 5

		for i := 0; i < attempts; i++ {
			result, err = cb.Execute(func() (interface{}, error) {
				return callExternalAPI()
			})
			if err == nil {
				// Increment success count in Prometheus
				requestCount.WithLabelValues("success").Inc()
				break
			}
			time.Sleep(exponentialBackoff(i))
		}

		if err != nil {
			// Increment failure count in Prometheus
			requestCount.WithLabelValues("failure").Inc()
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(fmt.Sprintf("Request succeeded: %v", result)))
	})

	fmt.Println("Starting server on :8111...")
	if err := http.ListenAndServe(":8111", nil); err != nil {
		fmt.Printf("Server failed to start: %v\n", err)
	}
}
