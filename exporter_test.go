package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-test/deep"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/log"

	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	log.Base().SetLevel("FATAL") // Suppress log messages during tests
	deep.CompareUnexportedFields = true
}

type director struct {
	*httptest.Server
	response []byte
}

func newDirector(response []byte) *director {
	d := &director{response: response}
	d.Server = httptest.NewServer(handler(d))
	return d
}

func handler(d *director) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write(d.response)
	}
}

func readCounter(m prometheus.Counter) float64 {
	// TODO: Revisit this once client_golang offers better testing tools.
	pb := &dto.Metric{}
	m.Write(pb)
	return pb.GetCounter().GetValue()
}

func readGauge(m prometheus.Gauge) float64 {
	// TODO: Revisit this once client_golang offers better testing tools.
	pb := &dto.Metric{}
	m.Write(pb)
	return pb.GetGauge().GetValue()
}

func TestInvalidScheme(t *testing.T) {
	e, err := NewExporter("gopher://gopher.quux.org", false, "", 1*time.Second)
	if expect, got := (*Exporter)(nil), e; expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if expect, got := err.Error(), `unsupported scheme: "gopher"`; expect != got {
		t.Errorf("expected %q, got %q", expect, got)
	}
}

func TestNotFound(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	url := strings.Replace(s.URL, "http://", "http://user:pass@", 1) // Prevent SOAP client errors
	defer s.Close()

	e, err := NewExporter(url, false, "", 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	ch := make(chan prometheus.Metric)
	go func() {
		defer close(ch)
		e.Collect(ch)
	}()

	if expect, got := 0., readGauge((<-ch).(prometheus.Gauge)); expect != got {
		// up
		t.Errorf("expected %f up, got %f", expect, got)
	}
	if expect, got := 1., readCounter((<-ch).(prometheus.Counter)); expect != got {
		// totalScrapes
		t.Errorf("expected %f total scrapes, got %f", expect, got)
	}
	if expect, got := 1., readCounter((<-ch).(prometheus.Counter)); expect != got {
		// failedScrapes
		t.Errorf("expected %f failed scrapes, got %f", expect, got)
	}
}
