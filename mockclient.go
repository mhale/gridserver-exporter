package main

import (
	"math/rand"
	"strconv"
	"time"
)

const (
	numBrokers = 5
)

// MockClient is a mock GridServer reporting data source.
// It is used to generate fake metrics data for testing with Prometheus.
type MockClient struct {
}

// NewMockClient returns a new MockClient.
func NewMockClient() *MockClient {
	return &MockClient{}
}

// Fetch generates random Broker reports and sums them to calculate an entire grid report.
func (m *MockClient) Fetch() func() (GridReport, []BrokerReport, error) {
	return func() (GridReport, []BrokerReport, error) {
		grid := GridReport{}
		brokers := []BrokerReport{}

		// Generate random Broker reports.
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for i := 1; i < numBrokers+1; i++ {
			totalEngines := 10000 + r.Intn(100)
			brokers = append(brokers, BrokerReport{
				Hostname:        "broker" + strconv.Itoa(i) + ".example.com",
				Name:            "BROKER_NAME_" + strconv.Itoa(i),
				BusyEngines:     r.Intn(totalEngines),
				TotalEngines:    totalEngines,
				Drivers:         r.Intn(10),
				ServicesRunning: r.Intn(50),
				TasksPending:    r.Intn(100000),
				UptimeMinutes:   r.Intn(10000),
			})
		}

		// Sum the individual Broker reports to calculate a whole grid report.
		for _, broker := range brokers {
			grid.BusyEngines += broker.BusyEngines
			grid.TotalEngines += broker.TotalEngines
			grid.Drivers += broker.Drivers
			grid.ServicesRunning += broker.ServicesRunning
			grid.TasksPending += broker.TasksPending
		}

		grid.TasksRunning = grid.BusyEngines

		return grid, brokers, nil
	}
}
