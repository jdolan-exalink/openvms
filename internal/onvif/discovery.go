package onvif

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	discoveryNamespace     = "http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01"
	addressingNamespace    = "http://www.w3.org/2005/08/addressing"
	discoveryProbeAction   = discoveryNamespace + "/Probe"
	discoveryMatchesAction = discoveryNamespace + "/ProbeMatches"
	maxDiscoveryPacket     = 64 << 10
)

var ErrDiscoveryTimeout = errors.New("ONVIF discovery timed out")

// DatagramTransport is an interface-scoped boundary. Implementations send and
// receive datagrams only on the requested local interface; tests inject fakes.
type DatagramTransport interface {
	Send(ctx context.Context, interfaceName string, payload []byte) error
	Receive(ctx context.Context, interfaceName string) ([]byte, error)
}

type DiscoveryConfig struct {
	Timeout    time.Duration
	MaxResults int
}

type Discovery struct {
	transport DatagramTransport
	config    DiscoveryConfig
}

func NewDiscovery(transport DatagramTransport, config DiscoveryConfig) *Discovery {
	if config.Timeout <= 0 {
		config.Timeout = 3 * time.Second
	}
	if config.Timeout > 30*time.Second {
		config.Timeout = 30 * time.Second
	}
	if config.MaxResults <= 0 {
		config.MaxResults = 32
	}
	if config.MaxResults > 256 {
		config.MaxResults = 256
	}
	return &Discovery{transport: transport, config: config}
}

// DiscoveredDevice contains untrusted discovery metadata. ScopeHints are not
// manufacturer identity, and XAddrs are validated but never contacted here.
type DiscoveredDevice struct {
	EndpointReference string
	XAddrs            []Endpoint
	Types             []string
	ScopeHints        []string
}

func (d *Discovery) Probe(ctx context.Context, interfaceName string) ([]DiscoveredDevice, error) {
	if d == nil || d.transport == nil || strings.TrimSpace(interfaceName) == "" {
		return nil, fmt.Errorf("invalid ONVIF discovery configuration")
	}
	messageID, err := newMessageID()
	if err != nil {
		return nil, fmt.Errorf("create discovery message identity: %w", err)
	}
	payload := buildProbe(messageID)
	probeCtx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	defer cancel()
	if err := d.transport.Send(probeCtx, interfaceName, payload); err != nil {
		return nil, fmt.Errorf("send ONVIF discovery probe: %w", err)
	}

	result := make([]DiscoveredDevice, 0)
	deviceIndex := make(map[string]int)
	seenAddresses := make(map[string]bool)
	for len(result) < d.config.MaxResults {
		packet, err := d.transport.Receive(probeCtx, interfaceName)
		if err != nil {
			if errors.Is(err, ErrDiscoveryTimeout) || errors.Is(err, context.DeadlineExceeded) {
				return result, nil
			}
			if errors.Is(err, context.Canceled) {
				return result, err
			}
			return result, fmt.Errorf("receive ONVIF discovery response: %w", err)
		}
		if len(packet) == 0 || len(packet) > maxDiscoveryPacket {
			continue
		}
		matches, ok := parseProbeMatches(packet, messageID)
		if !ok {
			continue
		}
		for _, match := range matches {
			if match.EndpointReference == "" {
				continue
			}
			i, exists := deviceIndex[match.EndpointReference]
			if !exists {
				if len(result) >= d.config.MaxResults {
					break
				}
				i = len(result)
				deviceIndex[match.EndpointReference] = i
				result = append(result, DiscoveredDevice{EndpointReference: match.EndpointReference, Types: match.Types, ScopeHints: match.ScopeHints})
			}
			for _, address := range match.XAddrs {
				key := match.EndpointReference + "\x00" + address.value
				if !seenAddresses[key] {
					seenAddresses[key] = true
					result[i].XAddrs = append(result[i].XAddrs, address)
				}
			}
		}
	}
	return result, nil
}

func newMessageID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return "urn:uuid:" + h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

func buildProbe(messageID string) []byte {
	return []byte(`<s:Envelope xmlns:s="` + soapNamespace + `" xmlns:a="` + addressingNamespace + `" xmlns:d="` + discoveryNamespace + `" xmlns:dn="http://www.onvif.org/ver10/network/wsdl"><s:Header><a:Action>` + discoveryProbeAction + `</a:Action><a:MessageID>` + messageID + `</a:MessageID><a:To>urn:docs-oasis-open-org:ws-dd:ns:discovery:2009:01</a:To><a:ReplyTo><a:Address>http://www.w3.org/2005/08/addressing/anonymous</a:Address></a:ReplyTo></s:Header><s:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></s:Body></s:Envelope>`)
}

type probeEnvelope struct {
	XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Header  struct {
		Action    string `xml:"http://www.w3.org/2005/08/addressing Action"`
		RelatesTo string `xml:"http://www.w3.org/2005/08/addressing RelatesTo"`
	} `xml:"http://www.w3.org/2003/05/soap-envelope Header"`
	Body struct {
		Matches struct {
			Items []probeMatchEntry `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 ProbeMatch"`
		} `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 ProbeMatches"`
	} `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}
type probeMatchEntry struct {
	EPR struct {
		Address string `xml:"http://www.w3.org/2005/08/addressing Address"`
	} `xml:"http://www.w3.org/2005/08/addressing EndpointReference"`
	Types  string `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 Types"`
	Scopes string `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 Scopes"`
	XAddrs string `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 XAddrs"`
}

type parsedProbeMatch struct {
	EndpointReference string
	XAddrs            []Endpoint
	Types             []string
	ScopeHints        []string
}

func parseProbeMatches(packet []byte, messageID string) ([]parsedProbeMatch, bool) {
	var envelope probeEnvelope
	if err := xml.Unmarshal(packet, &envelope); err != nil || envelope.XMLName.Space != soapNamespace || envelope.Header.Action != discoveryMatchesAction || strings.TrimSpace(envelope.Header.RelatesTo) != messageID {
		return nil, false
	}
	result := make([]parsedProbeMatch, 0, len(envelope.Body.Matches.Items))
	for _, m := range envelope.Body.Matches.Items {
		parsed := parsedProbeMatch{EndpointReference: strings.TrimSpace(m.EPR.Address), Types: strings.Fields(m.Types), ScopeHints: strings.Fields(m.Scopes)}
		for _, raw := range strings.Fields(m.XAddrs) {
			endpoint, err := ParseEndpoint(raw)
			if err == nil {
				parsed.XAddrs = append(parsed.XAddrs, endpoint)
			}
		}
		result = append(result, parsed)
	}
	return result, true
}
