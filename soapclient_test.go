package main

import (
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"testing"
	"time"

	"github.com/go-test/deep"
)

const (
	html            = `<html><head><title></title><body></body></html>` // Using the wrong path can get a 404 page
	invalidEnvelope = `<Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/">`
	soapFault       = `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
<soapenv:Body>
   <soapenv:Fault>
	  <faultcode xmlns:ns1="http://xml.apache.org/axis/">ns1:Server.NoService</faultcode>
	  <faultstring>The AXIS engine could not find a target service to invoke!  targetService is InvalidAdmin</faultstring>
	  <detail>
		 <ns2:hostname xmlns:ns2="http://xml.apache.org/axis/">director</ns2:hostname>
	  </detail>
   </soapenv:Fault>
</soapenv:Body>
</soapenv:Envelope>`
)

func newBrokerInfo(hostname string, brokerID int64, busyEngineCount, driverCount, engineCount int) *BrokerInfo {
	return &BrokerInfo{
		XMLName:         xml.Name{Space: "http://admin.gridserver.webservices.datasynapse.com", Local: "getAllBrokerInfoReturn"},
		BaseURL:         fmt.Sprintf("http://%s:8000/livecluster", hostname),
		BrokerID:        brokerID,
		BusyEngineCount: busyEngineCount,
		DriverCount:     driverCount,
		DriverWeight:    1.0,
		EngineCount:     engineCount,
		EngineWeight:    1.0,
		Failover:        false,
		Hostname:        fmt.Sprintf("http://%s:8000/livecluster", hostname),
		MaxEngines:      2500,
		Name:            hostname,
	}
}

func TestNewSOAPClient(t *testing.T) {
	type args struct {
		uri       string
		sslVerify bool
		timeout   time.Duration
	}
	tests := []struct {
		name    string
		args    args
		want    *SOAPClient
		wantErr bool
	}{
		{"FullPath",
			args{"http://user:pass@director:1234/livecluster/webservices", false, 5 * time.Second},
			&SOAPClient{"http://director:1234/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 5 * time.Second, false},
			false,
		},
		{"FullPathWithTimeout",
			args{"http://user:pass@director:1234/livecluster/webservices", false, 10 * time.Second},
			&SOAPClient{"http://director:1234/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 10 * time.Second, false},
			false,
		},
		{"SecureFullPathSkipVerify",
			args{"https://user:pass@director:1234/livecluster/webservices", false, 5 * time.Second},
			&SOAPClient{"https://director:1234/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 5 * time.Second, false},
			false,
		},
		{"SecureFullPathWithVerify",
			args{"https://user:pass@director:1234/livecluster/webservices", true, 5 * time.Second},
			&SOAPClient{"https://director:1234/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: false}, 5 * time.Second, false},
			false,
		},
		{"NoScheme",
			args{"user:pass@director:1234/livecluster/webservices", false, 5 * time.Second},
			nil,
			true,
		},
		{"InvalidScheme",
			args{"gopher://user:pass@gopher.quux.org", false, 5 * time.Second},
			nil,
			true,
		},
		{"NoUsername",
			args{"http://director:1234/livecluster/webservices", false, 5 * time.Second},
			nil,
			true,
		},
		{"NoPassword",
			args{"http://user@director:1234/livecluster/webservices", false, 5 * time.Second},
			nil,
			true,
		},
		{"NoHostname",
			args{"http://user:pass@", false, 5 * time.Second},
			nil,
			true,
		},
		{"NoPort",
			args{"http://user:pass@director/livecluster/webservices", false, 5 * time.Second},
			&SOAPClient{"http://director:8080/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 5 * time.Second, false},
			false,
		},
		{"InvalidPort",
			args{"http://user:pass@director:port/livecluster/webservices", false, 5 * time.Second},
			nil,
			true,
		},
		{"NoPath",
			args{"http://user:pass@director:1234", false, 5 * time.Second},
			&SOAPClient{"http://director:1234/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 5 * time.Second, false},
			false,
		},
		{"NoPortOrPath",
			args{"http://user:pass@director", false, 5 * time.Second},
			&SOAPClient{"http://director:8080/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 5 * time.Second, false},
			false,
		},
		{"SlashPath",
			args{"http://user:pass@director:1234/", false, 5 * time.Second},
			&SOAPClient{"http://director:1234/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 5 * time.Second, false},
			false,
		},
		{"LiveclusterWithSlash",
			args{"http://user:pass@director:1234/livecluster/", false, 5 * time.Second},
			&SOAPClient{"http://director:1234/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 5 * time.Second, false},
			false,
		},
		{"FullPathWithSlash",
			args{"http://user:pass@director:1234/livecluster/webservices/", false, 5 * time.Second},
			&SOAPClient{"http://director:1234/livecluster/webservices", "user", "pass", &tls.Config{InsecureSkipVerify: true}, 5 * time.Second, false},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSOAPClient(tt.args.uri, tt.args.sslVerify, tt.args.timeout, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSOAPClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("NewSOAPClient() = %v, want %v", got, tt.want)
				t.Errorf("Difference: %s", diff)
			}
		})
	}
}

func Test_cleanPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output string
	}{
		{"DefaultPath", "/livecluster/webservices", "/livecluster/webservices"},
		{"DefaultPathWithSlash", "/livecluster/webservices/", "/livecluster/webservices"},
		{"Root", "/", "/livecluster/webservices"},
		{"NoPath", "", "/livecluster/webservices"},
		{"HalfPath", "/livecluster", "/livecluster/webservices"},
		{"HalfPathWithSlash", "/livecluster/", "/livecluster/webservices"},
		{"Proxy", "/proxy", "/proxy"},
		{"ProxyWithSlash", "/proxy/", "/proxy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanPath(tt.input); got != tt.output {
				t.Errorf("cleanPath(%q) = %q, want %q", tt.input, got, tt.output)
			}
		})
	}
}

func TestSOAPClient_GetAllBrokerInfo(t *testing.T) {
	response := `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
	<soapenv:Body>
	   <getAllBrokerInfoResponse xmlns="http://admin.gridserver.webservices.datasynapse.com">
		  <getAllBrokerInfoReturn>
			 <baseUrl>http://broker:8000/livecluster</baseUrl>
			 <brokerId>1825975427</brokerId>
			 <busyEngineCount>%d</busyEngineCount>
			 <driverCount>%d</driverCount>
			 <driverRoutingConditions xsi:nil="true"/>
			 <driverWeight>1.0</driverWeight>
			 <engineCount>%d</engineCount>
			 <engineRoutingConditions xsi:nil="true"/>
			 <engineWeight>1.0</engineWeight>
			 <failover>false</failover>
			 <hostname>http://broker:8000/livecluster</hostname>
			 <maxEngines>2500</maxEngines>
			 <minEngines>0</minEngines>
			 <minIdleHomeEngines>0</minIdleHomeEngines>
			 <name>broker</name>
		  </getAllBrokerInfoReturn>
		  <getAllBrokerInfoReturn>
			 <baseUrl>http://broker2:8000/livecluster</baseUrl>
			 <brokerId>1179598041</brokerId>
			 <busyEngineCount>%d</busyEngineCount>
			 <driverCount>%d</driverCount>
			 <driverRoutingConditions xsi:nil="true"/>
			 <driverWeight>1.0</driverWeight>
			 <engineCount>%d</engineCount>
			 <engineRoutingConditions xsi:nil="true"/>
			 <engineWeight>1.0</engineWeight>
			 <failover>false</failover>
			 <hostname>http://broker2:8000/livecluster</hostname>
			 <maxEngines>2500</maxEngines>
			 <minEngines>0</minEngines>
			 <minIdleHomeEngines>0</minIdleHomeEngines>
			 <name>broker2</name>
		  </getAllBrokerInfoReturn>
	   </getAllBrokerInfoResponse>
	</soapenv:Body>
 </soapenv:Envelope>`
	tests := []struct {
		name     string
		response string
		want     []*BrokerInfo
		wantErr  bool
	}{
		{"NoEngines",
			fmt.Sprintf(response, 0, 0, 0, 0, 0, 0),
			[]*BrokerInfo{
				newBrokerInfo("broker", 1825975427, 0, 0, 0),
				newBrokerInfo("broker2", 1179598041, 0, 0, 0),
			},
			false,
		},
		{"SomeEngines",
			fmt.Sprintf(response, 1, 2, 3, 4, 5, 6),
			[]*BrokerInfo{
				newBrokerInfo("broker", 1825975427, 1, 2, 3),
				newBrokerInfo("broker2", 1179598041, 4, 5, 6),
			},
			false,
		},
		{"EmptyResponse", "", nil, true},
		{"InvalidEnvelope", invalidEnvelope, nil, true},
		{"Fault", soapFault, nil, true},
		{"HTML", html, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDirector([]byte(tt.response))
			s := &SOAPClient{
				URL: d.URL,
			}
			got, _, err := s.GetAllBrokerInfo()
			if (err != nil) != tt.wantErr {
				t.Errorf("SOAPClient.GetAllBrokerInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("SOAPClient.GetAllBrokerInfo() = %v, want %v", got, tt.want)
				t.Errorf("Difference: %s", diff)
			}
		})
	}
}

func TestSOAPClient_GetRunningServiceCount(t *testing.T) {
	response := `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
	<soapenv:Body>
	   <getRunningServiceCountResponse xmlns="http://admin.gridserver.webservices.datasynapse.com">
		  <getRunningServiceCountReturn>%d</getRunningServiceCountReturn>
	   </getRunningServiceCountResponse>
	</soapenv:Body>
 </soapenv:Envelope>`
	tests := []struct {
		name     string
		response string
		want     int
		wantErr  bool
	}{
		{"NoTasks", fmt.Sprintf(response, 0), 0, false},
		{"SomeTasks", fmt.Sprintf(response, 12345), 12345, false},
		{"EmptyResponse", "", -1, true},
		{"InvalidEnvelope", invalidEnvelope, -1, true},
		{"Fault", soapFault, -1, true},
		{"HTML", html, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDirector([]byte(tt.response))
			s := &SOAPClient{
				URL: d.URL,
			}
			got, _, err := s.GetRunningServiceCount(d.URL)
			if (err != nil) != tt.wantErr {
				t.Errorf("SOAPClient.GetRunningServiceCount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SOAPClient.GetRunningServiceCount() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSOAPClient_GetRunningInvocationCount(t *testing.T) {
	response := `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
	<soapenv:Body>
	   <getRunningInvocationCountResponse xmlns="http://admin.gridserver.webservices.datasynapse.com">
		  <getRunningInvocationCountReturn>%d</getRunningInvocationCountReturn>
	   </getRunningInvocationCountResponse>
	</soapenv:Body>
 </soapenv:Envelope>`
	tests := []struct {
		name     string
		response string
		want     int
		wantErr  bool
	}{
		{"NoTasks", fmt.Sprintf(response, 0), 0, false},
		{"SomeTasks", fmt.Sprintf(response, 12345), 12345, false},
		{"EmptyResponse", "", -1, true},
		{"InvalidEnvelope", invalidEnvelope, -1, true},
		{"Fault", soapFault, -1, true},
		{"HTML", html, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDirector([]byte(tt.response))
			s := &SOAPClient{
				URL: d.URL,
			}
			got, _, err := s.GetRunningInvocationCount(d.URL)
			if (err != nil) != tt.wantErr {
				t.Errorf("SOAPClient.GetRunningInvocationCount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SOAPClient.GetRunningInvocationCount() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSOAPClient_GetPendingInvocationCount(t *testing.T) {
	response := `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
	<soapenv:Body>
	   <getPendingInvocationCountResponse xmlns="http://admin.gridserver.webservices.datasynapse.com">
		  <getPendingInvocationCountReturn>%d</getPendingInvocationCountReturn>
	   </getPendingInvocationCountResponse>
	</soapenv:Body>
 </soapenv:Envelope>`
	tests := []struct {
		name     string
		response string
		want     int
		wantErr  bool
	}{
		{"NoTasks", fmt.Sprintf(response, 0), 0, false},
		{"SomeTasks", fmt.Sprintf(response, 12345), 12345, false},
		{"EmptyResponse", "", -1, true},
		{"InvalidEnvelope", invalidEnvelope, -1, true},
		{"Fault", soapFault, -1, true},
		{"HTML", html, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDirector([]byte(tt.response))
			s := &SOAPClient{
				URL: d.URL,
			}
			got, _, err := s.GetPendingInvocationCount(d.URL)
			if (err != nil) != tt.wantErr {
				t.Errorf("SOAPClient.GetPendingInvocationCount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SOAPClient.GetPendingInvocationCount() = %v, want %v", got, tt.want)
			}
		})
	}
}
