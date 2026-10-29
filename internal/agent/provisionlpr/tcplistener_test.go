package provisionlpr

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTCPServerReceivesAndProcessesXMLStream(t *testing.T) {
	cfg := DefaultConfig()
	receiver := NewReceiver(cfg, nil)

	var (
		mu     sync.Mutex
		plates []string
	)
	receiver.Subscribe(func(ev LPREvent) {
		mu.Lock()
		defer mu.Unlock()
		plates = append(plates, ev.Plate)
	})

	srv, err := NewTCPServer("127.0.0.1:0", receiver, nil)
	if err != nil {
		t.Fatalf("NewTCPServer error: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer srv.Close()

	addr := srv.Addr().String()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("net.Dial error: %v", err)
	}
	defer conn.Close()

	xmlMsg1 := `<?xml version="1.0" encoding="utf-8"?>
<config version="1.0" xmlns="http://www.tvt.net.cn/schemas/config.xsd">
  <deviceName>CamTCP1</deviceName>
  <items>
    <item>
      <plateNumber>AF111AA</plateNumber>
    </item>
  </items>
</config>`

	xmlMsg2 := `<?xml version="1.0" encoding="utf-8"?>
<config version="1.0" xmlns="http://www.tvt.net.cn/schemas/config.xsd">
  <deviceName>CamTCP1</deviceName>
  <items>
    <item>
      <plateNumber>BF222BB</plateNumber>
    </item>
  </items>
</config>`

	// Send in small chunks to simulate network streaming
	_, err = fmt.Fprint(conn, xmlMsg1[:30])
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	_, err = fmt.Fprint(conn, xmlMsg1[30:])
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	_, err = fmt.Fprint(conn, xmlMsg2)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := append([]string(nil), plates...)
	mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("expected 2 plates, got %d: %v", len(got), got)
	}
	if got[0] != "AF111AA" || got[1] != "BF222BB" {
		t.Fatalf("unexpected plates: %v", got)
	}
}
