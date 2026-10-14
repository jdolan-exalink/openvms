package onvif

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

const ptzNamespace = "http://www.onvif.org/ver20/ptz/wsdl"

// PTZClient provides ONVIF PTZ service operations.
type PTZClient struct {
	client   *Client
	endpoint Endpoint
}

func NewPTZClient(client *Client, endpoint Endpoint) *PTZClient {
	return &PTZClient{client: client, endpoint: endpoint}
}

type PTZPreset struct {
	Token string `xml:"token,attr" json:"token"`
	Name  string `xml:"http://www.onvif.org/ver10/schema Name" json:"name"`
}

func (p *PTZClient) ContinuousMove(ctx context.Context, profileToken string, panSpeed, tiltSpeed, zoomSpeed float64, timeoutSeconds int) error {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 5
	}
	content := fmt.Sprintf(`<tptz:ProfileToken>%s</tptz:ProfileToken>`+
		`<tptz:Velocity>`+
		`<tt:PanTilt xmlns:tt="http://www.onvif.org/ver10/schema" x="%f" y="%f" space="http://www.onvif.org/ver10/tptz/PanTiltSpaces/VelocityGenericSpace"/>`+
		`<tt:Zoom xmlns:tt="http://www.onvif.org/ver10/schema" x="%f" space="http://www.onvif.org/ver10/tptz/ZoomSpaces/VelocityGenericSpace"/>`+
		`</tptz:Velocity>`+
		`<tptz:Timeout>PT%dS</tptz:Timeout>`,
		profileToken, panSpeed, tiltSpeed, zoomSpeed, timeoutSeconds)

	_, err := p.call(ctx, "ContinuousMove", content, Mutating)
	return err
}

func (p *PTZClient) Stop(ctx context.Context, profileToken string, panTilt, zoom bool) error {
	content := fmt.Sprintf(`<tptz:ProfileToken>%s</tptz:ProfileToken>`+
		`<tptz:PanTilt>%t</tptz:PanTilt>`+
		`<tptz:Zoom>%t</tptz:Zoom>`,
		profileToken, panTilt, zoom)

	_, err := p.call(ctx, "Stop", content, Mutating)
	return err
}

func (p *PTZClient) AbsoluteMove(ctx context.Context, profileToken string, x, y, zoom float64) error {
	content := fmt.Sprintf(`<tptz:ProfileToken>%s</tptz:ProfileToken>`+
		`<tptz:Position>`+
		`<tt:PanTilt xmlns:tt="http://www.onvif.org/ver10/schema" x="%f" y="%f" space="http://www.onvif.org/ver10/tptz/PanTiltSpaces/PositionGenericSpace"/>`+
		`<tt:Zoom xmlns:tt="http://www.onvif.org/ver10/schema" x="%f" space="http://www.onvif.org/ver10/tptz/ZoomSpaces/PositionGenericSpace"/>`+
		`</tptz:Position>`,
		profileToken, x, y, zoom)

	_, err := p.call(ctx, "AbsoluteMove", content, Mutating)
	return err
}

func (p *PTZClient) GetPresets(ctx context.Context, profileToken string) ([]PTZPreset, error) {
	content := `<tptz:ProfileToken>` + profileToken + `</tptz:ProfileToken>`
	data, err := p.call(ctx, "GetPresets", content, ReadOnly)
	if err != nil {
		return nil, err
	}
	var response struct {
		Presets []PTZPreset `xml:"http://www.onvif.org/ver20/ptz/wsdl Preset"`
	}
	if err := decodePTZOperation(data, "GetPresetsResponse", &response); err != nil {
		return nil, &CallError{Status: StatusFailed, Code: "invalid_ptz_presets"}
	}
	return response.Presets, nil
}

func (p *PTZClient) GotoPreset(ctx context.Context, profileToken, presetToken string) error {
	content := fmt.Sprintf(`<tptz:ProfileToken>%s</tptz:ProfileToken>`+
		`<tptz:PresetToken>%s</tptz:PresetToken>`,
		profileToken, presetToken)

	_, err := p.call(ctx, "GotoPreset", content, Mutating)
	return err
}

func decodePTZOperation(data []byte, name string, target any) error {
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
		if !ok || start.Name.Space != ptzNamespace || start.Name.Local != name {
			continue
		}
		return decoder.DecodeElement(target, &start)
	}
}

func (p *PTZClient) call(ctx context.Context, action, content string, op Operation) ([]byte, error) {
	body := `<tptz:` + action + ` xmlns:tptz="` + ptzNamespace + `">` + content + `</tptz:` + action + `>`
	return p.client.Call(ctx, p.endpoint, ptzNamespace+"/"+action, body, op)
}
