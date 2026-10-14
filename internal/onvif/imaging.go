package onvif

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

const imagingNamespace = "http://www.onvif.org/ver20/imaging/wsdl"

// ImagingClient provides ONVIF Imaging service operations.
type ImagingClient struct {
	client   *Client
	endpoint Endpoint
}

func NewImagingClient(client *Client, endpoint Endpoint) *ImagingClient {
	return &ImagingClient{client: client, endpoint: endpoint}
}

type ImagingSettings struct {
	Brightness       float64           `xml:"http://www.onvif.org/ver10/schema Brightness" json:"brightness"`
	ColorSaturation  float64           `xml:"http://www.onvif.org/ver10/schema ColorSaturation" json:"color_saturation"`
	Contrast         float64           `xml:"http://www.onvif.org/ver10/schema Contrast" json:"contrast"`
	Sharpness        float64           `xml:"http://www.onvif.org/ver10/schema Sharpness" json:"sharpness"`
	IrCutFilter      string            `xml:"http://www.onvif.org/ver10/schema IrCutFilter" json:"ir_cut_filter"` // ON, OFF, AUTO
	WideDynamicRange *WideDynamicRange `xml:"http://www.onvif.org/ver10/schema WideDynamicRange" json:"wide_dynamic_range,omitempty"`
}

type WideDynamicRange struct {
	Mode  string  `xml:"http://www.onvif.org/ver10/schema Mode" json:"mode"` // ON, OFF
	Level float64 `xml:"http://www.onvif.org/ver10/schema Level" json:"level"`
}

func (i *ImagingClient) GetImagingSettings(ctx context.Context, videoSourceToken string) (ImagingSettings, error) {
	content := `<timg:VideoSourceToken>` + videoSourceToken + `</timg:VideoSourceToken>`
	data, err := i.call(ctx, "GetImagingSettings", content, ReadOnly)
	if err != nil {
		return ImagingSettings{}, err
	}
	var response struct {
		Settings ImagingSettings `xml:"http://www.onvif.org/ver20/imaging/wsdl ImagingSettings"`
	}
	if err := decodeImagingOperation(data, "GetImagingSettingsResponse", &response); err != nil {
		return ImagingSettings{}, &CallError{Status: StatusFailed, Code: "invalid_imaging_settings"}
	}
	return response.Settings, nil
}

func (i *ImagingClient) SetImagingSettings(ctx context.Context, videoSourceToken string, s ImagingSettings) error {
	wdrXML := ""
	if s.WideDynamicRange != nil {
		wdrXML = fmt.Sprintf(`<tt:WideDynamicRange xmlns:tt="http://www.onvif.org/ver10/schema"><tt:Mode>%s</tt:Mode><tt:Level>%f</tt:Level></tt:WideDynamicRange>`,
			s.WideDynamicRange.Mode, s.WideDynamicRange.Level)
	}
	content := fmt.Sprintf(`<timg:VideoSourceToken>%s</timg:VideoSourceToken>`+
		`<timg:ImagingSettings>`+
		`<tt:Brightness xmlns:tt="http://www.onvif.org/ver10/schema">%f</tt:Brightness>`+
		`<tt:ColorSaturation xmlns:tt="http://www.onvif.org/ver10/schema">%f</tt:ColorSaturation>`+
		`<tt:Contrast xmlns:tt="http://www.onvif.org/ver10/schema">%f</tt:Contrast>`+
		`<tt:Sharpness xmlns:tt="http://www.onvif.org/ver10/schema">%f</tt:Sharpness>`+
		`<tt:IrCutFilter xmlns:tt="http://www.onvif.org/ver10/schema">%s</tt:IrCutFilter>`+
		wdrXML+
		`</timg:ImagingSettings>`+
		`<timg:ForcePersistence>true</timg:ForcePersistence>`,
		videoSourceToken, s.Brightness, s.ColorSaturation, s.Contrast, s.Sharpness, s.IrCutFilter)

	_, err := i.call(ctx, "SetImagingSettings", content, Mutating)
	return err
}

func decodeImagingOperation(data []byte, name string, target any) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("missing SOAP operation %s", name)
			}
			return err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Space != imagingNamespace || start.Name.Local != name {
			continue
		}
		return decoder.DecodeElement(target, &start)
	}
}

func (i *ImagingClient) call(ctx context.Context, action, content string, op Operation) ([]byte, error) {
	body := `<timg:` + action + ` xmlns:timg="` + imagingNamespace + `">` + content + `</timg:` + action + `>`
	return i.client.Call(ctx, i.endpoint, imagingNamespace+"/"+action, body, op)
}
