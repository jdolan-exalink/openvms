package onvif

import (
	"context"
	"encoding/xml"
	"fmt"
)

const (
	deviceNamespace = "http://www.onvif.org/ver10/device/wsdl"
	schemaNamespace = "http://www.onvif.org/ver10/schema"
)

// DeviceClient provides read-only ONVIF Device service operations.
type DeviceClient struct {
	client   *Client
	endpoint Endpoint
}

func NewDeviceClient(client *Client, endpoint Endpoint) *DeviceClient {
	return &DeviceClient{client: client, endpoint: endpoint}
}

type DeviceInformation struct {
	Manufacturer    string `xml:"http://www.onvif.org/ver10/device/wsdl Manufacturer"`
	Model           string `xml:"http://www.onvif.org/ver10/device/wsdl Model"`
	FirmwareVersion string `xml:"http://www.onvif.org/ver10/device/wsdl FirmwareVersion"`
	SerialNumber    string `xml:"http://www.onvif.org/ver10/device/wsdl SerialNumber"`
	HardwareID      string `xml:"http://www.onvif.org/ver10/device/wsdl HardwareId"`
}

type ServiceVersion struct {
	Major int `xml:"http://www.onvif.org/ver10/schema Major"`
	Minor int `xml:"http://www.onvif.org/ver10/schema Minor"`
}

// Service endpoint strings are untrusted device-provided data. Callers must
// validate them before making a follow-up request.
type Service struct {
	Namespace string         `xml:"http://www.onvif.org/ver10/device/wsdl Namespace"`
	Endpoint  string         `xml:"http://www.onvif.org/ver10/device/wsdl XAddr"`
	Version   ServiceVersion `xml:"http://www.onvif.org/ver10/device/wsdl Version"`
}

func (s Service) ValidatedEndpoint() (Endpoint, error) {
	ep, err := ParseEndpoint(s.Endpoint)
	if err != nil {
		return Endpoint{}, fmt.Errorf("invalid ONVIF service endpoint: %w", ErrInvalidEndpoint)
	}
	return ep, nil
}

type DeviceClock struct {
	DateTimeType string        `xml:"http://www.onvif.org/ver10/schema DateTimeType"`
	UTC          DateTimeValue `xml:"http://www.onvif.org/ver10/schema UTCDateTime"`
	Local        DateTimeValue `xml:"http://www.onvif.org/ver10/schema LocalDateTime"`
}

type DateTimeValue struct {
	Time CalendarTime `xml:"http://www.onvif.org/ver10/schema Time"`
	Date CalendarDate `xml:"http://www.onvif.org/ver10/schema Date"`
}

type CalendarTime struct {
	Hour   int `xml:"http://www.onvif.org/ver10/schema Hour"`
	Minute int `xml:"http://www.onvif.org/ver10/schema Minute"`
	Second int `xml:"http://www.onvif.org/ver10/schema Second"`
}

type CalendarDate struct {
	Year  int `xml:"http://www.onvif.org/ver10/schema Year"`
	Month int `xml:"http://www.onvif.org/ver10/schema Month"`
	Day   int `xml:"http://www.onvif.org/ver10/schema Day"`
}

func (d *DeviceClient) GetDeviceInformation(ctx context.Context) (DeviceInformation, error) {
	data, err := d.call(ctx, "GetDeviceInformation", "")
	if err != nil {
		return DeviceInformation{}, err
	}
	var response struct{ XMLName xml.Name }
	var info DeviceInformation
	if err := xml.Unmarshal(data, &response); err != nil || response.XMLName.Local != "GetDeviceInformationResponse" {
		return DeviceInformation{}, &CallError{Status: StatusFailed, Code: "invalid_device_information"}
	}
	if err := xml.Unmarshal(data, &info); err != nil {
		return DeviceInformation{}, &CallError{Status: StatusFailed, Code: "invalid_device_information"}
	}
	return info, nil
}

func (d *DeviceClient) GetServices(ctx context.Context) ([]Service, error) {
	data, err := d.call(ctx, "GetServices", `<tds:IncludeCapability>false</tds:IncludeCapability>`)
	if err != nil {
		return nil, err
	}
	var response struct {
		XMLName  xml.Name
		Services []Service `xml:"http://www.onvif.org/ver10/device/wsdl Service"`
	}
	if err := xml.Unmarshal(data, &response); err != nil || response.XMLName.Local != "GetServicesResponse" {
		return nil, &CallError{Status: StatusFailed, Code: "invalid_services"}
	}
	return response.Services, nil
}

func (d *DeviceClient) GetSystemDateAndTime(ctx context.Context) (DeviceClock, error) {
	data, err := d.call(ctx, "GetSystemDateAndTime", "")
	if err != nil {
		return DeviceClock{}, err
	}
	var response struct {
		XMLName xml.Name
		Clock   DeviceClock `xml:"http://www.onvif.org/ver10/device/wsdl SystemDateAndTime"`
	}
	if err := xml.Unmarshal(data, &response); err != nil || response.XMLName.Local != "GetSystemDateAndTimeResponse" {
		return DeviceClock{}, &CallError{Status: StatusFailed, Code: "invalid_system_time"}
	}
	return response.Clock, nil
}

func (d *DeviceClient) call(ctx context.Context, action, content string) ([]byte, error) {
	body := `<tds:` + action + ` xmlns:tds="` + deviceNamespace + `">` + content + `</tds:` + action + `>`
	return d.client.Call(ctx, d.endpoint, deviceNamespace+"/"+action, body, ReadOnly)
}
