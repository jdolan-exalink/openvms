package onvif

import (
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeDatagrams struct {
	sent    [][]byte
	packets [][]byte
	reads   int
	onSend  func([]byte)
}

func (f *fakeDatagrams) Send(_ context.Context, _ string, p []byte) error {
	f.sent = append(f.sent, append([]byte(nil), p...))
	if f.onSend != nil {
		f.onSend(p)
	}
	return nil
}
func (f *fakeDatagrams) Receive(_ context.Context, _ string) ([]byte, error) {
	if f.reads >= len(f.packets) {
		return nil, ErrDiscoveryTimeout
	}
	p := f.packets[f.reads]
	f.reads++
	return p, nil
}

func TestDiscoveryProbeParsesCorrelatedMatchesAndDeduplicates(t *testing.T) {
	transport := &fakeDatagrams{}
	d := NewDiscovery(transport, DiscoveryConfig{Timeout: time.Second, MaxResults: 4})
	ctx := context.Background()
	// The transport provides a response after observing the request MessageID.
	transport.onSend = func(p []byte) {
		id := xmlValue(t, p, "MessageID")
		transport.packets = [][]byte{probeMatch(id, "urn:uuid:camera-1", "http://camera.local/onvif/device_service", "onvif://www.onvif.org/type/video_encoder"), probeMatch(id, "urn:uuid:camera-1", "http://camera.local/onvif/device_service", "onvif://www.onvif.org/type/video_encoder")}
	}
	devices, err := d.Probe(ctx, "eth-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].EndpointReference != "urn:uuid:camera-1" || len(devices[0].XAddrs) != 1 {
		t.Fatalf("unexpected devices: %+v", devices)
	}
	if !strings.Contains(string(transport.sent[0]), "2009/01/Probe") || !strings.Contains(string(transport.sent[0]), "NetworkVideoTransmitter") {
		t.Fatalf("not an ONVIF Probe: %s", transport.sent[0])
	}
	if !strings.HasPrefix(xmlValue(t, transport.sent[0], "MessageID"), "urn:uuid:") {
		t.Fatal("MessageID is not a UUID URN")
	}
}

func TestDiscoveryRejectsUnrelatedOrUnsafeResponses(t *testing.T) {
	cases := []struct {
		name     string
		response func(string) []byte
	}{
		{"wrong action", func(id string) []byte {
			return stringsToBytes(strings.Replace(string(probeMatch(id, "urn:uuid:a", "http://cam/device", "")), "ProbeMatches", "Hello", 1))
		}},
		{"wrong relation", func(string) []byte { return probeMatch("urn:uuid:other", "urn:uuid:a", "http://cam/device", "") }},
		{"unsafe xaddr", func(id string) []byte { return probeMatch(id, "urn:uuid:a", "http://user:pass@cam/device", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &fakeDatagrams{}
			tr.onSend = func(p []byte) { tr.packets = [][]byte{tc.response(xmlValue(t, p, "MessageID"))} }
			d := NewDiscovery(tr, DiscoveryConfig{Timeout: time.Second, MaxResults: 3})
			got, err := d.Probe(context.Background(), "eth-test")
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "unsafe xaddr" {
				if len(got) != 1 || len(got[0].XAddrs) != 0 {
					t.Fatalf("unsafe XAddr was not discarded: %+v", got)
				}
			} else if len(got) != 0 {
				t.Fatalf("accepted invalid response: %+v", got)
			}
		})
	}
}

func TestDiscoveryIgnoresOversizedDatagram(t *testing.T) {
	tr := &fakeDatagrams{}
	tr.onSend = func([]byte) { tr.packets = [][]byte{make([]byte, maxDiscoveryPacket+1)} }
	d := NewDiscovery(tr, DiscoveryConfig{Timeout: time.Second, MaxResults: 2})
	got, err := d.Probe(context.Background(), "eth-test")
	if err != nil || len(got) != 0 {
		t.Fatalf("oversized datagram was not bounded: devices=%v err=%v", got, err)
	}
}

func TestDiscoveryBoundsResultsAndTimeout(t *testing.T) {
	tr := &fakeDatagrams{}
	tr.onSend = func(p []byte) {
		id := xmlValue(t, p, "MessageID")
		tr.packets = [][]byte{probeMatch(id, "urn:uuid:a", "http://a/device", ""), probeMatch(id, "urn:uuid:b", "http://b/device", "")}
	}
	d := NewDiscovery(tr, DiscoveryConfig{Timeout: time.Second, MaxResults: 1})
	got, err := d.Probe(context.Background(), "eth-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected result bound, got %d", len(got))
	}
	tr = &fakeDatagrams{}
	d = NewDiscovery(tr, DiscoveryConfig{Timeout: time.Millisecond, MaxResults: 2})
	got, err = d.Probe(context.Background(), "eth-test")
	if err != nil || len(got) != 0 {
		t.Fatalf("want bounded empty discovery, got devices=%v err=%v", got, err)
	}
}

// Helpers intentionally produce SOAP only for deterministic parser tests.
func probeMatch(rel, epr, xaddr, scopes string) []byte {
	return stringsToBytes(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://www.w3.org/2005/08/addressing" xmlns:d="http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01"><s:Header><a:Action>http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01/ProbeMatches</a:Action><a:RelatesTo>` + rel + `</a:RelatesTo></s:Header><s:Body><d:ProbeMatches><d:ProbeMatch><a:EndpointReference><a:Address>` + epr + `</a:Address></a:EndpointReference><d:Types>dn:NetworkVideoTransmitter</d:Types><d:Scopes>` + scopes + `</d:Scopes><d:XAddrs>` + xaddr + `</d:XAddrs></d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
}
func stringsToBytes(s string) []byte { return []byte(s) }
func xmlValue(t *testing.T, data []byte, local string) string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == local {
			var value string
			if err := dec.DecodeElement(&value, &start); err != nil {
				t.Fatal(err)
			}
			return strings.TrimSpace(value)
		}
	}
	t.Fatalf("missing %s", local)
	return ""
}
