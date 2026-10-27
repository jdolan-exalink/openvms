package camconfig

import (
	"context"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/camconfig/catalog"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/frigatemock"
)

func TestRealFrigateAdapterReportsModelLabelsToTheCatalog(t *testing.T) {
	cams, _ := frigatemock.ParseCameras("patio")
	srv := &frigatemock.Server{Version: "0.17.2", Cameras: cams, Store: frigatemock.NewStore(1),
		User: "vms", Password: "pw", RequireAuth: true, StartedAt: time.Now()}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	a, err := frigate.Connect(context.Background(), frigate.ConnInfo{BaseURL: ts.URL, Username: "vms", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	cam, err := FrigateAdapter{Engine: a}.Read(context.Background(), Inventory{CameraID: "c1", RemoteName: "patio", Name: "Patio"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cam.Detection.AvailableLabels, "car") || len(cam.Detection.AvailableLabels) != 80 {
		t.Fatalf("labels: %v", cam.Detection.AvailableLabels)
	}
	cat := catalog.Default()
	for _, c := range cat.Categories {
		for _, o := range c.Objects {
			got := catalog.SupportFor(catalog.EngineFrigate, o, cam.Detection.AvailableLabels)
			if o.ID == "vehicle.car" && got != catalog.SupportYes {
				t.Errorf("car must be supported by COCO, got %v", got)
			}
			if o.ID == "vehicle.pickup" && got != catalog.SupportNo {
				t.Errorf("pickup has no Frigate label, got %v", got)
			}
		}
	}
	srv.Offline.Store(true)
	if got := ModelLabels(context.Background(), a); got != nil {
		t.Errorf("unreachable server must be unknown, got %v", got)
	}
}
