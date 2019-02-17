package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	"github.com/prometheus/procfs"
	"gopkg.in/alecthomas/kingpin.v2"
)

func main() {
	const helpText = `GridServer exporter for Prometheus.

Examples:
		gridserver-exporter -u http://username:password@host[:port][/path]
		gridserver-exporter -u https://username:password@host --no-tls-verify
		gridserver-exporter -u oracle://username:password@host:port/sid -s schema
		gridserver-exporter -u sqlserver://username:password@host/instance?database=databasename
		gridserver-exporter -u postgres://username:password@host/databasename?sslmode=disable
		gridserver-exporter -u mock://

`
	const pidFileHelpText = `Path to GridServer Manager PID file.
If provided, the standard process metrics get exported for the Manager
process, prefixed with 'gridserver_process_...'. The gridserver_process exporter
needs to have read access to files owned by the Manager process. Depends on
the availability of /proc.`

	var (
		listenAddress = kingpin.Flag("listen-address", "Address to listen on for web interface and telemetry.").Short('l').Default(":9343").Envar("GRIDSERVER_EXPORTER_LISTEN_ADDRESS").String()
		metricsPath   = kingpin.Flag("metrics-path", "Path under which to expose metrics.").Default("/metrics").Envar("GRIDSERVER_EXPORTER_METRICS_PATH").String()
		url           = kingpin.Flag("url", "URL for reporting database or Web Services (SOAP).").PlaceHolder("URL").Short('u').Required().Envar("GRIDSERVER_EXPORTER_URL").String()
		tlsVerify     = kingpin.Flag("tls-verify", "Flag that enables TLS certificate verification for the Web Services URL.").Default("true").Envar("GRIDSERVER_EXPORTER_TLS_VERIFY").Bool()
		schema        = kingpin.Flag("schema", "Schema name for reporting database.").PlaceHolder("SCHEMA").Short('s').Envar("GRIDSERVER_EXPORTER_SCHEMA").String()
		timeout       = kingpin.Flag("timeout", "Timeout for fetching metrics in seconds.").Short('t').Default("5s").Envar("GRIDSERVER_EXPORTER_TIMEOUT").Duration()
		once          = kingpin.Flag("once", "Fetch metrics once, then exit.").Default("false").Envar("GRIDSERVER_EXPORTER_ONCE").Bool()
		pidFile       = kingpin.Flag("pid-file", pidFileHelpText).PlaceHolder("FILENAME").Short('p').Envar("GRIDSERVER_EXPORTER_PID_FILE").String()
	)

	log.AddFlags(kingpin.CommandLine)
	kingpin.Version(version.Print("gridserver-exporter"))
	kingpin.CommandLine.Help = helpText
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	log.Infoln("Starting GridServer Exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())

	exporter, err := NewExporter(*url, *tlsVerify, *schema, *timeout)
	if err != nil {
		log.Fatalf("Start failed: %s", err)
	}

	if *once == true {
		start := time.Now()
		grid, _, err := exporter.Fetch()
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			log.With("elapsed", elapsed).Fatalf("Scrape failed: %s", err)
			return
		}
		log.With("elapsed", elapsed).
			With("busyEngines", grid.BusyEngines).
			With("drivers", grid.Drivers).
			With("servicesRunning", grid.ServicesRunning).
			With("tasksPending", grid.TasksPending).
			With("tasksRunning", grid.TasksRunning).
			With("totalEngines", grid.TotalEngines).
			Info("Scrape succeeded")
		return
	}

	prometheus.MustRegister(exporter)
	prometheus.MustRegister(version.NewCollector("gridserver_exporter"))

	if *pidFile != "" {
		if _, err := procfs.NewStat(); err != nil {
			log.Warn("Process metrics requested but not supported on this system")
		}

		// Set up process metric collection if supported by the runtime.
		procExporter := prometheus.NewProcessCollectorPIDFn(
			func() (int, error) {
				content, err := ioutil.ReadFile(*pidFile)
				if err != nil {
					return 0, fmt.Errorf("Can't read PID file: %s", err)
				}
				value, err := strconv.Atoi(strings.TrimSpace(string(content)))
				if err != nil {
					return 0, fmt.Errorf("Can't parse PID file: %s", err)
				}
				return value, nil
			}, namespace)
		prometheus.MustRegister(procExporter)
	}

	log.Infoln("Listening on", *listenAddress)
	http.Handle(*metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html>
			<html lang="en-US">
			<head>
				<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
				<title>GridServer Exporter for Prometheus</title>
			</head>
			<body>
				<h1>GridServer Exporter for Prometheus</h1>
				<p><a href='` + *metricsPath + `'>Metrics</a></p>
            </body>
            </html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
