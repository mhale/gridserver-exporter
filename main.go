package main

import (
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/procfs"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

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
	sourceURL     = kingpin.Flag("url", "URL for reporting database or Web Services (SOAP).").PlaceHolder("URL").Short('u').Required().Envar("GRIDSERVER_EXPORTER_URL").String()
	tlsVerify     = kingpin.Flag("tls-verify", "Enable or disable TLS certificate verification for the Web Services URL.").Default("true").Envar("GRIDSERVER_EXPORTER_TLS_VERIFY").Bool()
	schema        = kingpin.Flag("schema", "Schema name for reporting database.").PlaceHolder("SCHEMA").Short('s').Envar("GRIDSERVER_EXPORTER_SCHEMA").String()
	timeout       = kingpin.Flag("timeout", "Timeout for fetching metrics in seconds.").Short('t').Default("10s").Envar("GRIDSERVER_EXPORTER_TIMEOUT").Duration()
	once          = kingpin.Flag("once", "Fetch metrics once, then exit.").Default("false").Envar("GRIDSERVER_EXPORTER_ONCE").Bool()
	pidFile       = kingpin.Flag("pid-file", pidFileHelpText).PlaceHolder("FILENAME").Short('p').Envar("GRIDSERVER_EXPORTER_PID_FILE").String()
	logLevel      = kingpin.Flag("log-level", "Only log messages with the given severity or above. Valid levels: [fatal, error, warn, info, debug, trace]").Default("info").Envar("GRIDSERVER_EXPORTER_LOG_LEVEL").String()
	logFormat     = kingpin.Flag("log-format", `Set the log format. Valid formats: [text, json]"`).Default("text").Envar("GRIDSERVER_EXPORTER_LOG_FORMAT").String()
	logOutput     = kingpin.Flag("log-output", `Set the log output stream. Valid outputs: [stdout, stderr]`).Default("stderr").Envar("GRIDSERVER_EXPORTER_LOG_OUTPUT").String()
	directorOnly  = kingpin.Flag("director-only", "Restrict Web Services (SOAP) calls to the Director. Per-Broker service and task metrics will not be collected.").Default("false").Envar("GRIDSERVER_EXPORTER_DIRECTOR_ONLY").Bool()
)

// Middleware for logging hits to the web server.
func loggingHandler(h http.Handler) http.Handler {
	return loggingHandlerFunc(h.ServeHTTP)
}

func loggingHandlerFunc(h http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.WithField("remoteAddr", r.RemoteAddr).WithField("method", r.Method).WithField("url", r.URL.String()).WithField("host", r.Host).WithField("userAgent", r.UserAgent()).Debug("Exporter web server hit")
		h.ServeHTTP(w, r)
	})
}

// Handler for index page.
func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		newLevel := r.FormValue("level")
		level, err := log.ParseLevel(newLevel)
		if err != nil {
			log.WithField("level", newLevel).Error("Log level override failed")
		} else {
			log.SetLevel(level)
			oldLevel := *logLevel
			*logLevel = newLevel
			log.WithField("oldLevel", oldLevel).WithField("newLevel", newLevel).Info("Log level override succeeded")
		}
	}
	optionsHTML := ""
	logLevels := []string{"fatal", "error", "warn", "info", "debug", "trace"}
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
}

func main() {
	kingpin.Version(version.Print("gridserver-exporter"))
	kingpin.CommandLine.Help = helpText
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	switch *logOutput {
	case "stderr":
		log.SetOutput(os.Stderr)
	case "stdout":
		log.SetOutput(os.Stdout)
	default:
		log.WithField("output", *logOutput).Fatal("Invalid log output stream")
	}

	switch *logFormat {
	case "text":
		log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	default:
		log.WithField("format", *logFormat).Fatal("Invalid log format")
	}

	switch *logLevel {
	case "panic":
		log.WithField("level", *logLevel).Fatal("Invalid log level")
	default:
		level, err := log.ParseLevel(*logLevel)
		if err != nil {
			log.WithField("level", *logLevel).Fatal("Invalid log level")
		} else {
			log.SetLevel(level)
		}
	}

	log.WithField("version", version.Version).Info("Starting GridServer Exporter")
	log.WithField("go", version.GoVersion).
		WithField("user", version.BuildUser).
		WithField("date", version.BuildDate).
		WithField("branch", version.Branch).
		WithField("revision", version.Revision).
		Debug("Build context")

	exporter, err := NewExporter(*sourceURL, *tlsVerify, *schema, *timeout, *directorOnly)
	if err != nil {
		log.WithField("error", err).Fatal("Start failed")
	}

	// Fetch statistics once and exit if requested.
	if *once == true {
		start := time.Now()
		grid, brokers, err := exporter.Fetch()
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			log.WithField("elapsed", elapsed).WithField("error", err).Error("Scrape failed")
		} else {
			log.WithField("elapsed", elapsed).
				WithField("brokers", len(brokers)).
				WithField("busyEngines", grid.BusyEngines).
				WithField("drivers", grid.Drivers).
				WithField("servicesRunning", grid.ServicesRunning).
				WithField("tasksPending", grid.TasksPending).
				WithField("tasksRunning", grid.TasksRunning).
				WithField("totalEngines", grid.TotalEngines).
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
						log.WithField("pidfile", *pidFile).WithField("error", err).Error("PID file read failed")
						return 0, errors.Wrap(err, "PID file read failed")
					}
					value, err := strconv.Atoi(strings.TrimSpace(string(content)))
					if err != nil {
						log.WithField("pidfile", *pidFile).WithField("error", err).Error("PID file parse failed")
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
	http.Handle(*metricsPath, loggingHandler(promhttp.Handler()))
	http.HandleFunc("/", loggingHandlerFunc(indexHandler))
	http.Handle("/favicon.ico", loggingHandler(http.NotFoundHandler()))

	log.WithField("address", *listenAddress).WithField("path", *metricsPath).Info("Listening on network")
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
