package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	_ "github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	_ "gopkg.in/goracle.v2"
)

const (
	queryTmpl = `
		WITH latest AS (
			SELECT
				broker_id,
				MAX(time_stamp) AS max_time_stamp
			FROM %[1]s.broker_stats
			GROUP BY broker_id )
		SELECT
			latest.broker_id,
			brokers.broker_url,
			brokers.broker_name,
			broker_stats.num_busy_engines,
			broker_stats.num_total_engines,
			broker_stats.num_drivers,
			broker_stats.uptime_minutes,
			broker_stats.num_jobs_running,
			broker_stats.num_tasks_pending,
			latest.max_time_stamp AS time_stamp
		FROM latest
		INNER JOIN %[1]s.broker_stats ON %[1]s.broker_stats.broker_id = latest.broker_id
			AND %[1]s.broker_stats.time_stamp = latest.max_time_stamp
		INNER JOIN %[1]s.brokers ON %[1]s.brokers.broker_id = latest.broker_id
		`
)

// SQLClient is a custom SQL client specific to the GridServer reporting database.
type SQLClient struct {
	Driver  string
	DSN     string
	Schema  string
	Timeout time.Duration // Currently ignored - relying on the default timeouts in the driver instead
	db      *sql.DB
}

// NewSQLClient returns a new SQLClient configured for accessing a GridServer reporting database.
func NewSQLClient(uri string, schema string, timeout time.Duration) (*SQLClient, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, errors.Wrap(err, "invalid URL")
	}
	username := u.User.Username()
	if len(username) == 0 {
		return nil, fmt.Errorf("username not set")
	}
	_, set := u.User.Password()
	if !set {
		return nil, fmt.Errorf("password not set")
	}
	if len(u.Hostname()) == 0 {
		return nil, fmt.Errorf("hostname not set")
	}
	if len(u.Port()) > 0 {
		intPort, err := strconv.Atoi(u.Port())
		if err != nil || 0 > intPort || intPort > 65535 {
			return nil, fmt.Errorf("invalid port: %q", u.Port())
		}
	}

	var driver string
	var dsn string
	switch u.Scheme {
	case "postgres", "postgresql":
		if len(schema) == 0 {
			schema = "public" // Default schema on Postgres is "public"
		}
		driver = "postgres"
		u.Scheme = "postgres"
		dsn = u.String()
	case "mssql", "sqlserver":
		if len(schema) == 0 {
			schema = "dbo" // Default schema on SQL Server is "dbo"
		}
		driver = "sqlserver"
		u.Scheme = "sqlserver"
		dsn = u.String()
	case "ora", "oracle":
		if len(schema) == 0 {
			schema = u.User.Username() // Default schema on Oracle is the username
		}
		driver = "goracle"
		// Oracle DSNs look like: user/pass@host:port/sid - note the first slash
		password, _ := u.User.Password()
		dsn = fmt.Sprintf("%s/%s@%s:%s%s", u.User.Username(), password, u.Hostname(), u.Port(), u.Path)
	default:
		return nil, fmt.Errorf("unsupported scheme: %q", u.Scheme)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		log.With("driver", driver).With("error", err).Debug("Database client creation failed")
		return nil, errors.Wrap(err, "database client creation failed")
	}

	return &SQLClient{
		Driver:  driver,
		DSN:     dsn,
		Schema:  schema,
		Timeout: timeout,
		db:      db,
	}, nil
}

// Fetch retrieves the most recent Broker reports from the reporting database
// and sums them to calculate an entire grid report.
func (s *SQLClient) Fetch() func() (GridReport, []BrokerReport, error) {
	return func() (GridReport, []BrokerReport, error) {
		grid := GridReport{TasksRunning: -1}
		brokers := []BrokerReport{}

		start := time.Now()
		err := s.db.Ping()
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			log.With("elapsed", elapsed).With("error", err).Debug("Database connection failed")
			return grid, nil, errors.Wrap(err, "database connection failed")
		}
		log.With("elapsed", elapsed).Debug("Database connection succeeded")

		query := fmt.Sprintf(queryTmpl, s.Schema)        // Insert the schema
		query = strings.Join(strings.Fields(query), " ") // Remove the line breaks and tabs for logs

		start = time.Now()
		rows, err := s.db.Query(query)
		elapsed = time.Since(start).Round(time.Millisecond)
		if err != nil {
			log.With("elapsed", elapsed).With("error", err).With("sql", query).Debug("SQL query failed")
			return grid, nil, errors.Wrap(err, "SQL query failed")
		}
		defer rows.Close()
		log.With("elapsed", elapsed).Debug("SQL query succeeded")

		for rows.Next() {
			var brokerID int
			var brokerURL string
			var ts time.Time
			r := BrokerReport{}

			err = rows.Scan(&brokerID, &brokerURL, &r.Name, &r.BusyEngines, &r.TotalEngines, &r.Drivers, &r.UptimeMinutes, &r.ServicesRunning, &r.TasksPending, &ts)
			if err != nil {
				log.With("error", err).Debug("Row scan failed")
				return grid, nil, errors.Wrap(err, "row scan failed")
			}

			parsedURL, err := url.Parse(brokerURL)
			if err == nil {
				r.Hostname = parsedURL.Hostname()
			}

			brokers = append(brokers, r)

			// GridServer records a report every 30 seconds.
			// Log a warning if the timestamp is more than 60 seconds old.
			// This is likely to be a transient error e.g. during a reboot.
			age := time.Since(ts).Round(time.Second)
			if age > 1*time.Minute {
				log.With("age", age).With("hostname", r.Hostname).With("name", r.Name).With("id", brokerID).Warn("Most recent report for Broker is more than 60 seconds old")
			}
		}
		err = rows.Err()
		if err != nil {
			log.With("error", err).Debug("Row processing failed")
			return grid, nil, errors.Wrap(err, "row processing failed")
		}

		// Add up the individual broker reports to calculate an entire grid report.
		for _, broker := range brokers {
			grid.BusyEngines += broker.BusyEngines
			grid.TotalEngines += broker.TotalEngines
			grid.Drivers += broker.Drivers
			grid.ServicesRunning += broker.ServicesRunning
			grid.TasksPending += broker.TasksPending
		}

		return grid, brokers, nil
	}
}
