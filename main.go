package main

import (
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
		logLevel      = kingpin.Flag("log-level", "Only log messages with the given severity or above. Valid levels: [debug, info, warn, error, fatal]").Default("info").Envar("GRIDSERVER_EXPORTER_LOG_LEVEL").String()
		logFormat     = kingpin.Flag("log-format", `Set the log target and format. Example: "logger:syslog?appname=bob&local=7" or "logger:stdout?json=true"`).Default("logger:stderr").Envar("GRIDSERVER_EXPORTER_LOG_FORMAT").String()
	)

	kingpin.Version(version.Print("gridserver-exporter"))
	kingpin.CommandLine.Help = helpText
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	err := log.Base().SetFormat(*logFormat)
	if err != nil {
		log.With("format", *logFormat).With("error", err).Fatal("Invalid log format")
	}

	err = log.Base().SetLevel(*logLevel)
	if err != nil {
		log.With("level", *logLevel).With("error", err).Fatal("Invalid log level")
	}

	log.With("version", version.Version).Info("Starting GridServer Exporter")
	log.With("go", version.GoVersion).
		With("user", version.BuildUser).
		With("date", version.BuildDate).
		With("branch", version.Branch).
		With("revision", version.Revision).
		Debug("Build context")

	exporter, err := NewExporter(*url, *tlsVerify, *schema, *timeout)
	if err != nil {
		log.With("error", err).Fatal("Start failed")
	}

	// Fetch statistics once and exit if requested.
	if *once == true {
		start := time.Now()
		grid, _, err := exporter.Fetch()
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			log.With("elapsed", elapsed).With("error", err).Error("Scrape failed")
		} else {
			log.With("elapsed", elapsed).
				With("busyEngines", grid.BusyEngines).
				With("drivers", grid.Drivers).
				With("servicesRunning", grid.ServicesRunning).
				With("tasksPending", grid.TasksPending).
				With("tasksRunning", grid.TasksRunning).
				With("totalEngines", grid.TotalEngines).
				Info("Scrape succeeded")
		}
		return
	}

	prometheus.MustRegister(exporter)
	prometheus.MustRegister(version.NewCollector("gridserver_exporter"))

	// Configure process metric collection if supported by the runtime.
	if *pidFile != "" {
		if _, err := procfs.NewStat(); err != nil {
			log.Fatal("Process metrics requested but not supported on this system")
		} else {
			procExporter := prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{
				PidFn: func() (int, error) {
					content, err := ioutil.ReadFile(*pidFile)
					if err != nil {
						log.With("pidfile", *pidFile).With("error", err).Error("PID file read failed")
						return 0, errors.Wrap(err, "PID file read failed")
					}
					value, err := strconv.Atoi(strings.TrimSpace(string(content)))
					if err != nil {
						log.With("pidfile", *pidFile).With("error", err).Error("PID file parse failed")
						return 0, errors.Wrap(err, "PID file parse failed")
					}
					return value, nil
				},
				Namespace: namespace,
			})
			prometheus.MustRegister(procExporter)
		}
	}

	// Configure web server to be both browser and Prometheus friendly.
	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			level := r.FormValue("level")
			err := log.Base().SetLevel(level)
			if err != nil {
				log.With("error", err).Error("Log level override failed")
			} else {
				oldLevel := *logLevel
				*logLevel = level
				log.With("oldLevel", oldLevel).With("newLevel", level).Info("Log level override succeeded")
			}
		}
		optionsHTML := ""
		logLevels := []string{"debug", "info", "warn", "error", "fatal"}
		for _, level := range logLevels {
			if *logLevel == level {
				optionsHTML += "<option selected>" + level + "</option>"
			} else {
				optionsHTML += "<option>" + level + "</option>"
			}
		}
		w.Write([]byte(`<!doctype html>
			<html lang="en-US">
			<head>
				<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
				<title>GridServer Exporter for Prometheus</title>
			</head>
			<body>
				<h1>GridServer Exporter for Prometheus</h1>
				<p><a href="` + *metricsPath + `">Metrics</a></p>
				<form action="" method="post">
					<p>
						<label>Log Level:</label>
						&nbsp;
						<select name="level">
							` + optionsHTML + `
						</select>
						&nbsp;
						<button>Apply</button>
					</p>
				</form>
            </body>
            </html>`))
	})
	http.Handle("/favicon.ico", http.NotFoundHandler())

	log.With("address", *listenAddress).With("path", *metricsPath).Info("Listening on network")
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
