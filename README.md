# GridServer Exporter for Prometheus

This is a simple server that scrapes GridServer reporting statistics and exports them via HTTP for Prometheus consumption. It is based on the [official HAProxy Exporter](https://github.com/prometheus/haproxy_exporter).

## Getting Started

To run it:

```bash
gridserver-exporter -u URL [flags]
```

Help on flags:

```bash
gridserver-exporter -h
```

## Usage

There are three supported sources of reporting information: the GridServer reporting database, the Web Services API, and a mock data source that returns random values. The source type is specified by the schema in the `url` parameter.

To use the reporting database:

```bash
gridserver-exporter -u oracle://username:password@host[:port]/sid
gridserver-exporter -u sqlserver://username:password@host[:port]/instance?database=databasename
gridserver-exporter -u postgres://username:password@host[:port]/databasename
```

To use the Web Services API:

```bash
gridserver-exporter -u http://username:password@host[:port][/path]
gridserver-exporter -u https://username:password@host[:port][/path]
```

To use the mock data source:

```bash
gridserver-exporter -u mock://
```

The port and path in the URLs are optional. If they are not specified, the GridServer (for Web Services) or database driver (for the reporting database) defaults will be used.

### GridServer reporting database

The supported GridServer reporting databases are Oracle, SQL Server and PostgreSQL. The URL scheme will determine the driver used. The database schema used will be the default for the database, but can be overridden by the `schema` parameter.

```bash
gridserver-exporter -u oracle://username:password@host/sid -s schema
```

#### Oracle

The Oracle support uses the [godror](https://github.com/godror/godror) package.

Note that the username & password are colon-separated in the "url" parameter, not slash-separated as in the godror package documentation.

```bash
gridserver-exporter -u oracle://username:password@host/sid
```

For Oracle specific options, see the [godror Readme file](https://github.com/godror/godror).

#### SQL Server

The SQL Server support uses the [go-mssqldb](https://github.com/denisenkom/go-mssqldb) package.

```bash
gridserver-exporter -u sqlserver://username:password@host/instance?database=databasename
```

For SQL Server specific options, see the [go-mssqldb Readme file](https://github.com/denisenkom/go-mssqldb/blob/master/README.md).

#### PostgreSQL

The PostgreSQL support uses the [pq](https://github.com/lib/pq) package, which enables TLS by default. If the PostgreSQL server does not use TLS, it can be disabled with the `sslmode` query parameter.

```bash
gridserver-exporter -u postgres://username:password@host/databasename?sslmode=disable
```

For PostgreSQL specific options, see the [pq package documentation](https://godoc.org/github.com/lib/pq).

### GridServer Web Services API

The GridServer Web Services API can be accessed over HTTP or HTTPS. The URL supplied must specify the Director, as Brokers do not support the BrokerAdmin service.

```bash
gridserver-exporter -u http://username:password@host[:port][/path]
```

TLS certificate validation is enabled by default. It can be disabled using the `no-tls-verify` flag:

```bash
gridserver-exporter -u https://username:password@host --no-tls-verify
```

### Mock data

The mock data source is useful for testing purposes (e.g. testing Prometheus or Grafana) as it does not require a GridServer installation.

```bash
gridserver-exporter -u mock://
```

### Building

The godror package depends on [ODPI](https://github.com/oracle/odpi), which has [installation requirements](https://oracle.github.io/odpi/doc/installation.html).

The compile-time version information can be set with `ldflags`:

```bash
BRANCH=$(git rev-parse --abbrev-ref HEAD)
REVISION=$(git rev-parse HEAD)
DATE=$(date +%F)
go build -ldflags "-X github.com/prometheus/common/version.Branch=$BRANCH \
-X github.com/prometheus/common/version.Revision=$REVISION \
-X github.com/prometheus/common/version.BuildDate=$DATE \
-X github.com/prometheus/common/version.BuildUser=$USER"
```

### Vendoring

If the dependencies are vendored, `go mod vendor` will skip the `odpi` directory because it does not contain any .go files. This may be resolved with:

```bash
go get && cp -r ~/go/pkg/mod/github.com/godror/godror@v0.15.0/odpi vendor/github.com/godror/godror/godror@v0.15.0/
```

### Configuring

The `once` parameter causes an immediate fetch of statistics, followed by an exit. It is useful for validating credentials and configuration options.

```bash
gridserver-exporter -u http://username:password@host --once
```

## Known Issues

- The Oracle Instant Client installs signal handlers which may cause crashes when CTRL-C is entered. Adding the following flags to the `sqlnet.ora` file on the system may prevent the crashes.

```bash
DIAG_ADR_ENABLED=OFF
DIAG_DDE_ENABLED=FALSE
DIAG_SIGHANDLER_ENABLED=FALSE
```

- The `timeout` parameter is ignored by the SQL client functionality.

## License

Apache License 2.0, see [LICENSE](LICENSE).
