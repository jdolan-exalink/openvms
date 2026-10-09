package onvif

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDeviceInformationPreservesWrapperNamespaces(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "tds:GetDeviceInformation") {
			t.Fatalf("request body: %s", body)
		}
		return response(200, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Manufacturer>Acme</tds:Manufacturer><tds:Model>MockCam</tds:Model><tds:FirmwareVersion>1.2</tds:FirmwareVersion><tds:SerialNumber>secret-serial</tds:SerialNumber><tds:HardwareId>hw1</tds:HardwareId></tds:GetDeviceInformationResponse></s:Body></s:Envelope>`), nil
	})
	client := NewDeviceClient(NewClient(transport, Config{Timeout: time.Second}), mustEndpoint(t, "http://camera.local/onvif/device_service"))
	info, err := client.GetDeviceInformation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Manufacturer != "Acme" || info.Model != "MockCam" || info.FirmwareVersion != "1.2" || info.SerialNumber != "secret-serial" || info.HardwareID != "hw1" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestDeviceServicesAndSystemTime(t *testing.T) {
	responses := []string{
		`<tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace><tds:XAddr>http://camera.local/onvif/media</tds:XAddr><tds:Version><tt:Major xmlns:tt="http://www.onvif.org/ver10/schema">2</tt:Major><tt:Minor xmlns:tt="http://www.onvif.org/ver10/schema">0</tt:Minor></tds:Version></tds:Service></tds:GetServicesResponse>`,
		`<tds:GetSystemDateAndTimeResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:SystemDateAndTime><tt:DateTimeType xmlns:tt="http://www.onvif.org/ver10/schema">NTP</tt:DateTimeType><tt:UTCDateTime xmlns:tt="http://www.onvif.org/ver10/schema"><tt:Time><tt:Hour>12</tt:Hour><tt:Minute>34</tt:Minute><tt:Second>56</tt:Second></tt:Time><tt:Date><tt:Year>2025</tt:Year><tt:Month>1</tt:Month><tt:Day>2</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
	}
	calls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return response(200, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>`+responses[calls-1]+`</s:Body></s:Envelope>`), nil
	})
	client := NewDeviceClient(NewClient(transport, Config{Timeout: time.Second}), mustEndpoint(t, "http://camera.local/onvif/device_service"))
	services, err := client.GetServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 1 || services[0].Namespace != "http://www.onvif.org/ver10/media/wsdl" || services[0].Endpoint != "http://camera.local/onvif/media" || services[0].Version.Major != 2 {
		t.Fatalf("unexpected services: %+v", services)
	}
	if _, err := services[0].ValidatedEndpoint(); err != nil {
		t.Fatalf("valid service address rejected: %v", err)
	}
	clock, err := client.GetSystemDateAndTime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if clock.DateTimeType != "NTP" || clock.UTC.Date.Year != 2025 || clock.UTC.Date.Month != 1 || clock.UTC.Date.Day != 2 || clock.UTC.Time.Hour != 12 || clock.UTC.Time.Minute != 34 || clock.UTC.Time.Second != 56 {
		t.Fatalf("unexpected time: %+v", clock)
	}
}

func TestServiceEndpointRejectsCredentialsAndQuery(t *testing.T) {
	svc := Service{Endpoint: "http://user:pass@camera.local/onvif/media?token=x"}
	if _, err := svc.ValidatedEndpoint(); err == nil {
		t.Fatal("unsafe service endpoint accepted")
	}
}

func mustEndpoint(t *testing.T, raw string) Endpoint {
	t.Helper()
	ep, err := ParseEndpoint(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ep
}

func TestDeviceInformationUsesNamespacesInheritedFromSOAPEnvelope(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><s:Body><tds:GetDeviceInformationResponse><tds:Manufacturer>Inherited Acme</tds:Manufacturer><tds:Model>InheritedCam</tds:Model></tds:GetDeviceInformationResponse></s:Body></s:Envelope>`), nil
	})
	client := NewDeviceClient(NewClient(transport, Config{Timeout: time.Second}), mustEndpoint(t, "http://camera.local/onvif/device_service"))
	info, err := client.GetDeviceInformation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Manufacturer != "Inherited Acme" || info.Model != "InheritedCam" {
		t.Fatalf("inherited namespace fields not decoded: %+v", info)
	}
}

func TestDeviceInformationUsesNamespacesInheritedFromSOAPBody(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:GetDeviceInformationResponse><tds:Manufacturer>Body Acme</tds:Manufacturer><tds:Model>BodyCam</tds:Model></tds:GetDeviceInformationResponse></s:Body></s:Envelope>`), nil
	})
	client := NewDeviceClient(NewClient(transport, Config{Timeout: time.Second}), mustEndpoint(t, "http://camera.local/onvif/device_service"))
	info, err := client.GetDeviceInformation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Manufacturer != "Body Acme" || info.Model != "BodyCam" {
		t.Fatalf("Body-inherited namespace fields not decoded: %+v", info)
	}
}
