package main

import (
	"fmt"
	"math"
	"net/url"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

const (
	namespace = "gridserver"
)

// GridReport represents a snapshot of the current state of the entire grid.
type GridReport struct {
	BusyEngines     int
	TotalEngines    int
	Drivers         int
	ServicesRunning int
	TasksRunning    int // Only reported via SOAP.
	TasksPending    int
}

// BrokerReport represents a snapshot of the current state of an individual Broker.
type BrokerReport struct {
	Hostname        string
	Name            string
	BusyEngines     int
	TotalEngines    int
	Drivers         int
	ServicesRunning int     // Only reported via SQL.
	TasksRunning    int     // Only reported via SOAP.
	TasksPending    int     // Only reported via SQL.
	UptimeMinutes   float64 // Only reported via SQL.
}

func newGridMetric(metricName string, docString string, constLabels prometheus.Labels) prometheus.Gauge {
	return prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "grid_" + metricName,
			Help:        docString,
			ConstLabels: constLabels,
		},
	)
}

func newBrokerMetric(metricName string, docString string, constLabels prometheus.Labels) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "broker_" + metricName,
			Help:        docString,
			ConstLabels: constLabels,
		},
		[]string{"name", "hostname"},
	)
}

// Exporter collects GridServer statistics from the given data source and exports them using the Prometheus metrics package.
type Exporter struct {
	URI                         string
	Fetch                       func() (GridReport, []BrokerReport, error)
	mutex                       sync.RWMutex
	up                          prometheus.Gauge
	totalScrapes, failedScrapes prometheus.Counter
	gridMetrics                 map[string]prometheus.Gauge
	brokerMetrics               map[string]*prometheus.GaugeVec
}

// NewExporter returns an initialized Exporter.
func NewExporter(uri string, sslVerify bool, schema string, timeout time.Duration, directorOnly bool) (*Exporter, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, errors.Wrap(err, "invalid URL")
	}

	// Determine which client to use.
	var fetch func() (GridReport, []BrokerReport, error)
	switch u.Scheme {
	case "http", "https":
		client, err := NewSOAPClient(uri, sslVerify, timeout, directorOnly)
		if err != nil {
			log.With("error", err).Debug("SOAP client creation failed")
			return nil, errors.Wrap(err, "SOAP client creation failed")
		}
		fetch = client.Fetch()
		u.User = url.User(u.User.Username()) // Filter password from logs
		log.With("url", u.String()).With("sslVerify", sslVerify).With("timeout", timeout).Info("Using Web Services API")
	case "postgres", "postgresql", "mssql", "sqlserver", "ora", "oracle":
		client, err := NewSQLClient(uri, schema, timeout)
		if err != nil {
			log.With("error", err).Debug("SQL client creation failed")
			return nil, errors.Wrap(err, "SQL client creation failed")
		}
		fetch = client.Fetch()
		u.User = url.User(u.User.Username()) // Filter password from logs
		log.With("url", u.String()).With("driver", client.Driver).With("schema", client.Schema).Info("Using reporting database")
	case "mock":
		client := NewMockClient()
		fetch = client.Fetch()
		log.Info("Using mock data")
	default:
		return nil, fmt.Errorf("unsupported scheme: %q", u.Scheme)
	}

	return &Exporter{
		URI:   uri,
		Fetch: fetch,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the last scrape of GridServer successful.",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_total_scrapes",
			Help:      "Total number of GridServer scrapes.",
		}),
		failedScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_failed_scrapes",
			Help:      "Number of failed GridServer scrapes.",
		}),
		gridMetrics: map[string]prometheus.Gauge{
			"busy_engines":     newGridMetric("busy_engines", "Number of Engines busy.", nil),
			"total_engines":    newGridMetric("total_engines", "Number of Engines logged in.", nil),
			"drivers":          newGridMetric("drivers", "Number of Drivers logged in.", nil),
			"services_running": newGridMetric("services_running", "Number of Services running.", nil),
			"tasks_running":    newGridMetric("tasks_running", "Number of tasks running.", nil),
			"tasks_pending":    newGridMetric("tasks_pending", "Number of tasks pending (not yet assigned to Engines).", nil),
		},
		brokerMetrics: map[string]*prometheus.GaugeVec{
			"busy_engines":     newBrokerMetric("busy_engines", "Number of Engines busy.", nil),
			"total_engines":    newBrokerMetric("total_engines", "Number of Engines logged in.", nil),
			"drivers":          newBrokerMetric("drivers", "Number of Drivers logged in.", nil),
			"services_running": newBrokerMetric("services_running", "Number of Services running.", nil),
			"tasks_running":    newBrokerMetric("tasks_running", "Number of tasks running.", nil),
			"tasks_pending":    newBrokerMetric("tasks_pending", "Number of tasks pending (not yet assigned to Engines).", nil),
			"uptime_minutes":   newBrokerMetric("uptime_minutes", "Time since Broker start in minutes.", nil),
		},
	}, nil
}

// Describe describes all the metrics reported by the GridServer exporter. It implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range e.gridMetrics {
		m.Describe(ch)
	}
	for _, m := range e.brokerMetrics {
		m.Describe(ch)
	}
	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
	ch <- e.failedScrapes.Desc()
}

// Collect fetches metrics from the configured GridServer reporting data source and delivers them
// as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock() // To protect metrics from concurrent collects.
	defer e.mutex.Unlock()

	e.resetMetrics()
	e.scrape()

	ch <- e.up
	ch <- e.totalScrapes
	ch <- e.failedScrapes
	e.collectMetrics(ch)
}

func (e *Exporter) scrape() {
	e.totalScrapes.Inc()

	start := time.Now()
	grid, brokers, err := e.Fetch()
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		e.up.Set(0)
		e.failedScrapes.Inc()
		log.With("elapsed", elapsed).With("error", err).Error("Scrape failed")
		return
	}
	e.up.Set(1)

	log.With("elapsed", elapsed).
		With("brokers", len(brokers)).
		With("busyEngines", grid.BusyEngines).
		With("totalEngines", grid.TotalEngines).
		With("drivers", grid.Drivers).
		With("servicesRunning", grid.ServicesRunning).
		With("tasksRunning", grid.TasksRunning).
		With("tasksPending", grid.TasksPending).
		Info("Scrape succeeded")

	e.gridMetrics["busy_engines"].Set(float64(grid.BusyEngines))
	e.gridMetrics["total_engines"].Set(float64(grid.TotalEngines))
	e.gridMetrics["drivers"].Set(float64(grid.Drivers))
	e.gridMetrics["services_running"].Set(float64(grid.ServicesRunning))
	// TasksRunning is only reported via SOAP.
	if grid.TasksRunning >= 0 {
		e.gridMetrics["tasks_running"].Set(float64(grid.TasksRunning))
	}
	e.gridMetrics["tasks_pending"].Set(float64(grid.TasksPending))

	for _, broker := range brokers {
		e.brokerMetrics["busy_engines"].WithLabelValues(broker.Name, broker.Hostname).Set(float64(broker.BusyEngines))
		e.brokerMetrics["total_engines"].WithLabelValues(broker.Name, broker.Hostname).Set(float64(broker.TotalEngines))
		e.brokerMetrics["drivers"].WithLabelValues(broker.Name, broker.Hostname).Set(float64(broker.Drivers))
		// ServicesRunning is only reported via SQL.
		if broker.ServicesRunning >= 0 {
			e.brokerMetrics["services_running"].WithLabelValues(broker.Name, broker.Hostname).Set(float64(broker.ServicesRunning))
		}
		// TasksRunning is only reported via SOAP.
		if broker.TasksRunning >= 0 {
			e.brokerMetrics["tasks_running"].WithLabelValues(broker.Name, broker.Hostname).Set(float64(broker.TasksRunning))
		}
		// TasksPending is only reported via SQL.
		if broker.TasksPending >= 0 {
			e.brokerMetrics["tasks_pending"].WithLabelValues(broker.Name, broker.Hostname).Set(float64(broker.TasksPending))
		}
		// Uptime is only reported via SQL.
		if broker.UptimeMinutes >= 0 {
			e.brokerMetrics["uptime_minutes"].WithLabelValues(broker.Name, broker.Hostname).Set(float64(broker.UptimeMinutes))
		}

		log.With("hostname", broker.Hostname).
			With("name", broker.Name).
			With("busyEngines", broker.BusyEngines).
			With("totalEngines", broker.TotalEngines).
			With("drivers", broker.Drivers).
			With("servicesRunning", broker.ServicesRunning).
			With("tasksRunning", broker.TasksRunning).
			With("tasksPending", broker.TasksPending).
			With("uptimeMinutes", broker.UptimeMinutes).
			Debug("Broker metrics processed")
	}
}

func (e *Exporter) resetMetrics() {
	for _, m := range e.gridMetrics {
		m.Set(math.NaN())
	}
	for _, m := range e.brokerMetrics {
		m.Reset()
	}
}

func (e *Exporter) collectMetrics(metrics chan<- prometheus.Metric) {
	for _, m := range e.gridMetrics {
		m.Collect(metrics)
	}
	for _, m := range e.brokerMetrics {
		m.Collect(metrics)
	}
}
