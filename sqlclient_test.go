package main

import (
	"database/sql"
	"testing"
	"time"

	"github.com/go-test/deep"
)

func newDB(driver, dsn string) *sql.DB {
	db, _ := sql.Open(driver, dsn)
	return db
}

func TestNewSQLClient(t *testing.T) {
	type args struct {
		uri     string
		schema  string
		timeout time.Duration
	}
	tests := []struct {
		name    string
		args    args
		want    *SQLClient
		wantErr bool
	}{
		{"PostgresFullPathNoSchema",
			args{"postgres://user:pass@director:1234/reporting", "", 5 * time.Second},
			&SQLClient{"postgres", "postgres://user:pass@director:1234/reporting", "public", 5 * time.Second,
				newDB("postgres", "postgres://user:pass@director:1234/reporting")},
			false,
		},
		{"PostgresqlFullPathNoSchema",
			args{"postgresql://user:pass@director:1234/reporting", "", 5 * time.Second},
			&SQLClient{"postgres", "postgres://user:pass@director:1234/reporting", "public", 5 * time.Second,
				newDB("postgres", "postgres://user:pass@director:1234/reporting")},
			false,
		},
		{"MSSQLFullPathNoSchema",
			args{"mssql://user:pass@director:1234/reporting", "", 5 * time.Second},
			&SQLClient{"sqlserver", "sqlserver://user:pass@director:1234/reporting", "dbo", 5 * time.Second,
				newDB("sqlserver", "sqlserver://user:pass@director:1234/reporting")},
			false,
		},
		{"SQLServerFullPathNoSchema",
			args{"sqlserver://user:pass@director:1234/reporting", "", 5 * time.Second},
			&SQLClient{"sqlserver", "sqlserver://user:pass@director:1234/reporting", "dbo", 5 * time.Second,
				newDB("sqlserver", "sqlserver://user:pass@director:1234/reporting")},
			false,
		},
		{"OraFullPathWithSchema",
			args{"ora://user:pass@director:1234/reporting", "foo", 5 * time.Second},
			&SQLClient{"godror", "user/pass@director:1234/reporting", "foo", 5 * time.Second,
				newDB("godror", "user/pass@director:1234/reporting")},
			false,
		},
		{"OracleFullPathWithSchema",
			args{"oracle://user:pass@director:1234/reporting", "foo", 5 * time.Second},
			&SQLClient{"godror", "user/pass@director:1234/reporting", "foo", 5 * time.Second,
				newDB("godror", "user/pass@director:1234/reporting")},
			false,
		},
		{"FullPathWithSchema",
			args{"postgres://user:pass@director:1234/reporting", "foo", 5 * time.Second},
			&SQLClient{"postgres", "postgres://user:pass@director:1234/reporting", "foo", 5 * time.Second,
				newDB("postgres", "postgres://user:pass@director:1234/reporting")},
			false,
		},
		{"FullPathWithTimeout",
			args{"postgres://user:pass@director:1234/reporting", "foo", 10 * time.Second},
			&SQLClient{"postgres", "postgres://user:pass@director:1234/reporting", "foo", 10 * time.Second,
				newDB("postgres", "postgres://user:pass@director:1234/reporting")},
			false,
		},
		{"NoScheme",
			args{"user:pass@director:1234/reporting", "", 5 * time.Second},
			nil,
			true,
		},
		{"InvalidScheme",
			args{"gopher://user:pass@gopher.quux.org", "", 5 * time.Second},
			nil,
			true,
		},
		{"NoUsername",
			args{"postgres://director:1234/reporting", "", 5 * time.Second},
			nil,
			true,
		},
		{"NoPassword",
			args{"postgres://user@director:1234/reporting", "", 5 * time.Second},
			nil,
			true,
		},
		{"BlankPassword",
			args{"postgres://user:@director:1234/reporting", "", 5 * time.Second},
			&SQLClient{"postgres", "postgres://user:@director:1234/reporting", "public", 5 * time.Second,
				newDB("postgres", "postgres://user:@director:1234/reporting")},
			false,
		},
		{"NoHostname",
			args{"postgres://user:pass@", "", 5 * time.Second},
			nil,
			true,
		},
		{"NoPort",
			args{"postgres://user:pass@director/reporting", "", 5 * time.Second},
			&SQLClient{"postgres", "postgres://user:pass@director/reporting", "public", 5 * time.Second,
				newDB("postgres", "postgres://user:pass@director/reporting")},
			false,
		},
		{"InvalidPort",
			args{"postgres://user:pass@director:port/reporting", "", 5 * time.Second},
			nil,
			true,
		},
		{"NoPath",
			args{"postgres://user:pass@director:1234", "", 5 * time.Second},
			&SQLClient{"postgres", "postgres://user:pass@director:1234", "public", 5 * time.Second,
				newDB("postgres", "postgres://user:pass@director:1234")},
			false,
		},
		{"NoPortOrPath",
			args{"postgres://user:pass@director", "", 5 * time.Second},
			&SQLClient{"postgres", "postgres://user:pass@director", "public", 5 * time.Second,
				newDB("postgres", "postgres://user:pass@director")},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSQLClient(tt.args.uri, tt.args.schema, tt.args.timeout)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSQLClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("NewSQLClient() = %v, want %v", got, tt.want)
				t.Errorf("Difference: %s", diff)
			}
		})
	}
}
