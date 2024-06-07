package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sony/gobreaker"
)

func TestCircuitBreakerV3(t *testing.T) {
	// Create a mock server to simulate external API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	// Replace callExternalAPI with a function that calls the mock server
	callExternalAPI = func() (int, error) {
		resp, err := http.Get(server.URL)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}

	// Configure circuit breaker settings for testing
	settings := gobreaker.Settings{
		Name:        "API Circuit Breaker",
		MaxRequests: 5,
		Interval:    60 * time.Second,
		Timeout:     5 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 3
		},
	}

	cb := gobreaker.NewCircuitBreaker(settings)

	t.Run("SuccessfulRequest", func(t *testing.T) {
		_, err := cb.Execute(func() (interface{}, error) {
			return callExternalAPI()
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	//Simulates consecutive failed requests and checks if the circuit breaker trips to the open state.
	t.Run("FailedRequests", func(t *testing.T) {
		// Override callExternalAPI to simulate failure
		callExternalAPI = func() (int, error) {
			return 0, errors.New("simulated failure")
		}

		for i := 0; i < 4; i++ {
			_, err := cb.Execute(func() (interface{}, error) {
				return callExternalAPI()
			})
			if err == nil {
				t.Fatalf("expected error, got none")
			}
		}

		if cb.State() != gobreaker.StateOpen {
			t.Fatalf("expected circuit breaker to be open, got %v", cb.State())
		}
	})

	//Simulates the circuit breaker being open,
	//waits for the timeout,
	//then checks if it closes again after a successful request.
	t.Run("RetryAfterTimeout", func(t *testing.T) {
		// Simulate circuit breaker opening
		callExternalAPI = func() (int, error) {
			return 0, errors.New("simulated failure")
		}

		for i := 0; i < 4; i++ {
			_, err := cb.Execute(func() (interface{}, error) {
				return callExternalAPI()
			})
			if err == nil {
				t.Fatalf("expected error, got none")
			}
		}

		if cb.State() != gobreaker.StateOpen {
			t.Fatalf("expected circuit breaker to be open, got %v", cb.State())
		}

		// Wait for timeout duration
		time.Sleep(settings.Timeout + 1*time.Second)

		//After the timeout period,
		//the circuit breaker should transition to the half-open state.

		// Restore original callExternalAPI to simulate success
		callExternalAPI = func() (int, error) {
			resp, err := http.Get(server.URL)
			if err != nil {
				return 0, err
			}
			defer resp.Body.Close()
			return resp.StatusCode, nil
		}

		_, err := cb.Execute(func() (interface{}, error) {
			return callExternalAPI()
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if cb.State() != gobreaker.StateHalfOpen {
			t.Fatalf("expected circuit breaker to be half-open, got %v", cb.State())
		}

		//After verifying the half-open state, another successful request is simulated to ensure the circuit breaker transitions back to the closed state.
		for i := 0; i < int(settings.MaxRequests); i++ {
			_, err = cb.Execute(func() (interface{}, error) {
				return callExternalAPI()
			})
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		}

		if cb.State() != gobreaker.StateClosed {
			t.Fatalf("expected circuit breaker to be closed, got %v", cb.State())
		}
	})

	t.Run("OnStateChange", func(t *testing.T) {
		stateChanges := []gobreaker.State{}
		settings.OnStateChange = func(name string, from gobreaker.State, to gobreaker.State) {
			stateChanges = append(stateChanges, to)
		}

		cb = gobreaker.NewCircuitBreaker(settings)

		// Simulate failures to trip the circuit breaker
		callExternalAPI = func() (int, error) {
			return 0, errors.New("simulated failure")
		}
		for i := 0; i < 4; i++ {
			_, err := cb.Execute(func() (interface{}, error) {
				return callExternalAPI()
			})
			if err == nil {
				t.Fatalf("expected error, got none")
			}
		}

		// Check for state transitions
		expectedStates := []gobreaker.State{gobreaker.StateOpen}
		if len(stateChanges) != len(expectedStates) {
			t.Fatalf("expected state changes %v, got %v", expectedStates, stateChanges)
		}
		for i, state := range expectedStates {
			if stateChanges[i] != state {
				t.Fatalf("expected state change to %v, got %v", state, stateChanges[i])
			}
		}
	})

	t.Run("ReadyToTrip", func(t *testing.T) {
		failures := 0
		settings.ReadyToTrip = func(counts gobreaker.Counts) bool {
			failures = int(counts.ConsecutiveFailures)
			return counts.ConsecutiveFailures > 2 // Trip after 2 failures
		}

		cb = gobreaker.NewCircuitBreaker(settings)

		// Simulate failures
		callExternalAPI = func() (int, error) {
			return 0, errors.New("simulated failure")
		}
		for i := 0; i < 3; i++ {
			_, err := cb.Execute(func() (interface{}, error) {
				return callExternalAPI()
			})
			if err == nil {
				t.Fatalf("expected error, got none")
			}
		}

		if failures != 3 {
			t.Fatalf("expected 3 consecutive failures, got %d", failures)
		}
		if cb.State() != gobreaker.StateOpen {
			t.Fatalf("expected circuit breaker to be open, got %v", cb.State())
		}
	})
}
