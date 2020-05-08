package main

import (
	"bytes"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
)

const (
	defaultPort = "8080"
	defaultPath = "/livecluster/webservices"
)

var client *http.Client // Global client to enable connection reuse

// BrokerInfo is a modified BrokerInfo SOAP type that ignores the routing-related fields.
type BrokerInfo struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getAllBrokerInfoReturn"`

	BaseURL                  string  `xml:"baseUrl,omitempty"`
	BrokerID                 int64   `xml:"brokerId,omitempty"`
	BusyEngineCount          int     `xml:"busyEngineCount,omitempty"`
	DriverCount              int     `xml:"driverCount,omitempty"`
	DriverRoutingComparators string  `xml:"-"`
	DriverRoutingConditions  string  `xml:"-"`
	DriverWeight             float64 `xml:"driverWeight,omitempty"`
	EngineCount              int     `xml:"engineCount,omitempty"`
	EngineRoutingComparators string  `xml:"-"`
	EngineRoutingConditions  string  `xml:"-"`
	EngineWeight             float64 `xml:"engineWeight,omitempty"`
	Failover                 bool    `xml:"failover,omitempty"`
	Hostname                 string  `xml:"hostname,omitempty"`
	MaxEngines               int     `xml:"maxEngines,omitempty"`
	MinEngines               int     `xml:"minEngines,omitempty"`
	MinIdleHomeEngines       int     `xml:"minIdleHomeEngines,omitempty"`
	Name                     string  `xml:"name,omitempty"`
}

// GetAllBrokerInfo represents a request to the getAllBrokerInfo() operation.
type GetAllBrokerInfo struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getAllBrokerInfo"`
}

// GetAllBrokerInfoResponse represents a response from the getAllBrokerInfo() operation.
type GetAllBrokerInfoResponse struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getAllBrokerInfoResponse"`

	BrokerInfos []*BrokerInfo `xml:"getAllBrokerInfoReturn,omitempty"`
}

// GetRunningServiceCount represents a request to the getRunningServiceCount() operation.
type GetRunningServiceCount struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getRunningServiceCount"`
}

// GetRunningServiceCountResponse represents a response from the getRunningServiceCount() operation.
type GetRunningServiceCountResponse struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getRunningServiceCountResponse"`

	GetRunningServiceCountReturn int `xml:"getRunningServiceCountReturn,omitempty"`
}

// GetRunningInvocationCount represents a request to the getRunningInvocationCount() operation.
type GetRunningInvocationCount struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getRunningInvocationCount"`
}

// GetRunningInvocationCountResponse represents a response from the getRunningInvocationCount() operation.
type GetRunningInvocationCountResponse struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getRunningInvocationCountResponse"`

	GetRunningInvocationCountReturn int `xml:"getRunningInvocationCountReturn,omitempty"`
}

// GetPendingInvocationCount represents a request to the getPendingInvocationCount() operation.
type GetPendingInvocationCount struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getPendingInvocationCount"`
}

// GetPendingInvocationCountResponse represents a response from the getPendingInvocationCount() operation.
type GetPendingInvocationCountResponse struct {
	XMLName xml.Name `xml:"http://admin.gridserver.webservices.datasynapse.com getPendingInvocationCountResponse"`

	GetPendingInvocationCountReturn int `xml:"getPendingInvocationCountReturn,omitempty"`
}

// SOAPEnvelope represents a SOAP Envelope for XML decoding.
type SOAPEnvelope struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`

	Header *SOAPHeader
	Body   SOAPBody
}

// SOAPHeader represents a SOAP Header for XML decoding.
type SOAPHeader struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Header"`

	Items []interface{} `xml:",omitempty"`
}

// SOAPBody represents a SOAP Body for XML decoding.
type SOAPBody struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Body"`

	Fault   *SOAPFault  `xml:",omitempty"`
	Content interface{} `xml:",omitempty"`
}

// UnmarshalXML converts the XML elements in a SOAP Body into Go types.
func (b *SOAPBody) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if b.Content == nil {
		return xml.UnmarshalError("Content must be a pointer to a struct")
	}

	var (
		token    xml.Token
		err      error
		consumed bool
	)

Loop:
	for {
		if token, err = d.Token(); err != nil {
			return err
		}

		if token == nil {
			break
		}

		switch se := token.(type) {
		case xml.StartElement:
			if consumed {
				return xml.UnmarshalError("Found multiple elements inside SOAP body; not wrapped-document/literal WS-I compliant")
			} else if se.Name.Space == "http://schemas.xmlsoap.org/soap/envelope/" && se.Name.Local == "Fault" {
				b.Fault = &SOAPFault{}
				b.Content = nil

				err = d.DecodeElement(b.Fault, &se)
				if err != nil {
					return err
				}

				consumed = true
			} else {
				if err = d.DecodeElement(b.Content, &se); err != nil {
					return err
				}

				consumed = true
			}
		case xml.EndElement:
			break Loop
		}
	}

	return nil
}

// SOAPFault represents a SOAP Fault for XML decoding.
type SOAPFault struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Fault"`

	Code   string `xml:"faultcode,omitempty"`
	String string `xml:"faultstring,omitempty"`
	Actor  string `xml:"faultactor,omitempty"`
	Detail string `xml:"detail,omitempty"`
}

func (f *SOAPFault) Error() string {
	return f.String
}

// SOAPClient is a custom SOAP client specific to GridServer Web Services.
type SOAPClient struct {
	URL          string
	Username     string
	Password     string
	TLSConfig    *tls.Config
	Timeout      time.Duration
	DirectorOnly bool
}

// NewSOAPClient returns a new SOAPClient configured for accessing a GridServer Manager.
func NewSOAPClient(uri string, tlsVerify bool, timeout time.Duration, directorOnly bool) (*SOAPClient, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, errors.Wrap(err, "invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %q", u.Scheme)
	}
	username := u.User.Username()
	if len(username) == 0 {
		return nil, fmt.Errorf("username not set")
	}
	password, set := u.User.Password()
	if !set {
		return nil, fmt.Errorf("password not set")
	}
	if len(u.Hostname()) == 0 {
		return nil, fmt.Errorf("hostname not set")
	}
	port := defaultPort
	if len(u.Port()) > 0 {
		intPort, err := strconv.Atoi(u.Port())
		if err != nil || 0 > intPort || intPort > 65535 {
			return nil, fmt.Errorf("invalid port: %q", u.Port())
		}
		port = strconv.Itoa(intPort)
	}

	director := &url.URL{
		Scheme: u.Scheme,
		Host:   net.JoinHostPort(u.Hostname(), port),
		Path:   cleanPath(u.Path),
	}
	tlsCfg := &tls.Config{
		InsecureSkipVerify: !tlsVerify,
	}

	tr := &http.Transport{
		TLSClientConfig: tlsCfg,
		Dial: (&net.Dialer{
			Timeout: timeout, // Use the user-specified timeout for the connection timeout
		}).Dial,
	}

	client = &http.Client{
		Transport: tr,
		Timeout:   timeout + 10*time.Millisecond, // Ensure connection timeout fires before request timeout
	}

	return &SOAPClient{
		URL:          director.String(),
		Username:     username,
		Password:     password,
		TLSConfig:    tlsCfg,
		Timeout:      timeout,
		DirectorOnly: directorOnly,
	}, nil
}

// cleanPath attempts to clean up the supplied path.
func cleanPath(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" || trimmed == "livecluster" {
		return defaultPath
	}
	return strings.TrimRight(path, "/")
}

// Call calls the requested operation.
func (s *SOAPClient) Call(url string, request, response interface{}) error {
	// Create SOAP request envelope.
	envelope := SOAPEnvelope{}
	envelope.Body.Content = request
	buffer := new(bytes.Buffer)
	encoder := xml.NewEncoder(buffer)
	if err := encoder.Encode(envelope); err != nil {
		return err
	}
	if err := encoder.Flush(); err != nil {
		return err
	}

	// Preserve request XML for later logging (Do() empties the buffer).
	reqXML := buffer.String()
	//log.With("request", reqXML).Debug("SOAP request prepared")

	// Create HTTP request.
	req, err := http.NewRequest("POST", url, buffer)
	if err != nil {
		log.With("error", err).With("request", reqXML).With("url", url).Debug("HTTP request creation failed")
		return errors.Wrap(err, "HTTP request creation failed")
	}
	req.SetBasicAuth(s.Username, s.Password)
	req.Header.Add("Content-Type", "text/xml; charset=\"utf-8\"")
	req.Header.Add("SOAPAction", "")
	req.Header.Set("User-Agent", "gridserver-exporter/"+version.Version)
	req.Close = true

	// Transmit HTTP request.
	res, err := client.Do(req)
	if err != nil {
		log.With("url", url).With("error", err).Debug("HTTP request failed")
		return errors.Wrap(err, "HTTP request failed")
	}
	defer res.Body.Close()

	// Receive HTTP response.
	rawbody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.With("error", err).With("request", reqXML).With("response", string(rawbody)).With("url", url).Debug("HTTP response body read failed")
		return errors.Wrap(err, "HTTP response body read failed")
	}
	if len(rawbody) == 0 {
		return fmt.Errorf("received empty response from server")
	}

	//log.With("response", string(rawbody)).Debug("SOAP response received")

	// Parse SOAP response envelope.
	respEnvelope := new(SOAPEnvelope)
	respEnvelope.Body = SOAPBody{Content: response}
	err = xml.Unmarshal(rawbody, respEnvelope)
	if err != nil {
		log.With("error", err).With("request", reqXML).With("response", string(rawbody)).With("url", url).Debug("Received invalid SOAP response")
		return errors.Wrap(err, "received invalid SOAP response")
	}

	// Check for faults.
	fault := respEnvelope.Body.Fault
	if fault != nil {
		log.With("fault", fault).With("request", reqXML).With("response", string(rawbody)).With("url", url).Debug("Received SOAP fault")
		return errors.Wrap(fault, "received SOAP fault")
	}

	return nil
}

// TimedCall wraps the Call function to measure its duration.
func (s *SOAPClient) TimedCall(url string, request, response interface{}) (elapsed time.Duration, err error) {
	start := time.Now()
	err = s.Call(url, request, response)
	elapsed = time.Since(start).Round(time.Millisecond)
	return
}

// GetAllBrokerInfo returns the current snapshot of broker information by calling the GetAllBrokerInfo operation.
func (s *SOAPClient) GetAllBrokerInfo() ([]*BrokerInfo, time.Duration, error) {
	endpoint := s.URL + "/BrokerAdmin"
	response := new(GetAllBrokerInfoResponse)
	elapsed, err := s.TimedCall(endpoint, new(GetAllBrokerInfo), response)
	if err != nil {
		return nil, elapsed, errors.Wrap(err, "SOAP call failed")
	}
	return response.BrokerInfos, elapsed, nil
}

// GetRunningServiceCount returns the current number of running services across all brokers by calling the GetRunningServiceCount operation.
func (s *SOAPClient) GetRunningServiceCount(endpoint string) (int, time.Duration, error) {
	response := new(GetRunningServiceCountResponse)
	elapsed, err := s.TimedCall(endpoint, new(GetRunningServiceCount), response)
	if err != nil {
		return -1, elapsed, errors.Wrap(err, "SOAP call failed")
	}
	return response.GetRunningServiceCountReturn, elapsed, nil
}

// GetRunningInvocationCount returns the current number of running tasks across all brokers by calling the GetRunningInvocationCount operation.
func (s *SOAPClient) GetRunningInvocationCount(endpoint string) (int, time.Duration, error) {
	response := new(GetRunningInvocationCountResponse)
	elapsed, err := s.TimedCall(endpoint, new(GetRunningInvocationCount), response)
	if err != nil {
		return -1, elapsed, errors.Wrap(err, "SOAP call failed")
	}
	return response.GetRunningInvocationCountReturn, elapsed, nil
}

// GetPendingInvocationCount returns the current number of pending tasks across all brokers by calling the GetPendingInvocationCount operation.
func (s *SOAPClient) GetPendingInvocationCount(endpoint string) (int, time.Duration, error) {
	response := new(GetPendingInvocationCountResponse)
	elapsed, err := s.TimedCall(endpoint, new(GetPendingInvocationCount), response)
	if err != nil {
		return -1, elapsed, errors.Wrap(err, "SOAP call failed")
	}
	return response.GetPendingInvocationCountReturn, elapsed, nil
}

// Fetch retrieves the most recent Broker and grid reports from the Web Services API.
func (s *SOAPClient) Fetch() func() (GridReport, []BrokerReport, error) {
	return func() (GridReport, []BrokerReport, error) {
		grid := GridReport{}
		brokers := []BrokerReport{}
		director, _ := url.Parse(s.URL)
		hostname := director.Hostname()

		// Get the Brokers and their basic metrics from the Director.
		brokerInfos, elapsed, err := s.GetAllBrokerInfo()
		if err != nil {
			log.With("elapsed", elapsed).With("hostname", hostname).With("error", err).Debug("BrokerAdmin.getAllBrokerInfo failed")
			return grid, nil, errors.Wrap(err, "BrokerAdmin.getAllBrokerInfo failed")
		}
		log.With("elapsed", elapsed).With("hostname", hostname).With("brokers", len(brokerInfos)).Debug("BrokerAdmin.getAllBrokerInfo succeeded")

		for _, brokerInfo := range brokerInfos {
			baseURL, _ := url.Parse(brokerInfo.BaseURL)
			broker := BrokerReport{
				Name:            brokerInfo.Name,
				Hostname:        baseURL.Hostname(),
				BusyEngines:     brokerInfo.BusyEngineCount,
				TotalEngines:    brokerInfo.EngineCount,
				Drivers:         brokerInfo.DriverCount,
				ServicesRunning: -1,
				TasksRunning:    -1,
				TasksPending:    -1,
				UptimeMinutes:   -1,
			}

			// If not operating in Director only mode, collect the per-Broker metrics.
			if !s.DirectorOnly {
				endpoint := brokerInfo.BaseURL + "/webservices/ServiceAdmin"

				broker.ServicesRunning, elapsed, err = s.GetRunningServiceCount(endpoint)
				if err != nil {
					log.With("elapsed", elapsed).With("hostname", broker.Hostname).With("name", broker.Name).With("error", err).Debug("ServiceAdmin.getRunningServiceCount failed")
					return grid, nil, errors.Wrap(err, "ServiceAdmin.getRunningServiceCount failed")
				}
				log.With("elapsed", elapsed).
					With("hostname", broker.Hostname).
					With("name", broker.Name).
					With("servicesRunning", broker.ServicesRunning).
					Debug("ServiceAdmin.getRunningServiceCount succeeded")

				broker.TasksRunning, elapsed, err = s.GetRunningInvocationCount(endpoint)
				if err != nil {
					log.With("elapsed", elapsed).With("hostname", broker.Hostname).With("name", broker.Name).With("error", err).Debug("ServiceAdmin.getRunningInvocationCount failed")
					return grid, nil, errors.Wrap(err, "ServiceAdmin.getRunningInvocationCount failed")
				}
				log.With("elapsed", elapsed).
					With("hostname", broker.Hostname).
					With("name", broker.Name).
					With("tasksRunning", broker.TasksRunning).
					Debug("ServiceAdmin.getRunningInvocationCount succeeded")

				broker.TasksPending, elapsed, err = s.GetPendingInvocationCount(endpoint)
				if err != nil {
					log.With("elapsed", elapsed).With("hostname", broker.Hostname).With("name", broker.Name).With("error", err).Debug("ServiceAdmin.getPendingInvocationCount failed")
					return grid, nil, errors.Wrap(err, "ServiceAdmin.getPendingInvocationCount failed")
				}
				log.With("elapsed", elapsed).
					With("hostname", broker.Hostname).
					With("name", broker.Name).
					With("tasksPending", broker.TasksPending).
					Debug("ServiceAdmin.getPendingInvocationCount succeeded")
			}

			brokers = append(brokers, broker)
		}

		// Sum the individual broker reports to calculate a whole grid report.
		for _, broker := range brokers {
			grid.BusyEngines += broker.BusyEngines
			grid.TotalEngines += broker.TotalEngines
			grid.Drivers += broker.Drivers

			// If not operating in Director only mode, use the per-Broker metrics.
			if !s.DirectorOnly {
				grid.ServicesRunning += broker.ServicesRunning
				grid.TasksRunning += broker.TasksRunning
				grid.TasksPending += broker.TasksPending
			}
		}

		// If operating in Director only mode, the following metrics will not have been obtained from the Brokers,
		// but they can be obtained at the grid level from the Director.
		if s.DirectorOnly {
			endpoint := s.URL + "/ManagerAdmin"

			grid.ServicesRunning, elapsed, err = s.GetRunningServiceCount(endpoint)
			if err != nil {
				log.With("elapsed", elapsed).With("hostname", hostname).With("error", err).Debug("ManagerAdmin.getRunningServiceCount failed")
				return grid, nil, errors.Wrap(err, "ManagerAdmin.getRunningServiceCount failed")
			}
			log.With("elapsed", elapsed).With("hostname", hostname).With("servicesRunning", grid.ServicesRunning).Debug("ManagerAdmin.getRunningServiceCount succeeded")

			grid.TasksRunning, elapsed, err = s.GetRunningInvocationCount(endpoint)
			if err != nil {
				log.With("elapsed", elapsed).With("hostname", hostname).With("error", err).Debug("ManagerAdmin.getRunningInvocationCount failed")
				return grid, nil, errors.Wrap(err, "ManagerAdmin.getRunningInvocationCount failed")
			}
			log.With("elapsed", elapsed).With("hostname", hostname).With("tasksRunning", grid.TasksRunning).Debug("ManagerAdmin.getRunningInvocationCount succeeded")

			grid.TasksPending, elapsed, err = s.GetPendingInvocationCount(endpoint)
			if err != nil {
				log.With("elapsed", elapsed).With("hostname", hostname).With("error", err).Debug("ManagerAdmin.getPendingInvocationCount failed")
				return grid, nil, errors.Wrap(err, "ManagerAdmin.getPendingInvocationCount failed")
			}
			log.With("elapsed", elapsed).With("hostname", hostname).With("tasksPending", grid.TasksPending).Debug("ManagerAdmin.getPendingInvocationCount succeeded")
		}

		return grid, brokers, nil
	}
}
