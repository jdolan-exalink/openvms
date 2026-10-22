//go:build integration

package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

type streamCodecs struct{ live, hq string }

func codecsOf(t *testing.T, env *demofix.Env, id uuid.UUID) streamCodecs {
	t.Helper()
	var c streamCodecs
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT live_codec, hq_codec FROM cameras WHERE id = $1`, id).Scan(&c.live, &c.hq)
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func streamsOf(t *testing.T, env *demofix.Env, id uuid.UUID) (live, hq string) {
	t.Helper()
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT live_stream, hq_stream FROM cameras WHERE id = $1`, id).Scan(&live, &hq)
	})
	if err != nil {
		t.Fatal(err)
	}
	return live, hq
}

func TestHealthPollerStoresTheStreamCodecsGo2RTCReports(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"]
	live, hq := streamsOf(t, env, cam.ID)
	if live == "" || hq == "" {
		t.Fatalf("the demo camera has no streams (live=%q hq=%q)", live, hq)
	}
	if got := codecsOf(t, env, cam.ID); got != (streamCodecs{}) {
		t.Fatalf("a fresh camera has unknown codecs, got %+v", got)
	}

	poller := &inventory.HealthPoller{Svc: env.Svc, Interval: time.Second, Concurrency: 2, CapabilityInterval: time.Nanosecond}

	// go2rtc reports H.265 on the live stream and H.264 on the hq one.
	mock.Server.Go2RTCStreams = []string{live, hq}
	mock.Server.Go2RTCCodecs = map[string]string{live: "H265", hq: "H264"}
	poller.PollOnce(ctx)
	if got := codecsOf(t, env, cam.ID); got != (streamCodecs{live: "h265", hq: "h264"}) {
		t.Fatalf("after the refresh: %+v", got)
	}

	// The camera is switched to H.264: the next refresh follows.
	mock.Server.Go2RTCCodecs = map[string]string{live: "H264", hq: "H264"}
	time.Sleep(5 * time.Millisecond)
	poller.PollOnce(ctx)
	if got := codecsOf(t, env, cam.ID); got != (streamCodecs{live: "h264", hq: "h264"}) {
		t.Fatalf("after the codec changed: %+v", got)
	}

	// A stream with no producer right now (idle) must not erase what is known.
	mock.Server.Go2RTCCodecs = map[string]string{hq: "H264"}
	time.Sleep(5 * time.Millisecond)
	poller.PollOnce(ctx)
	if got := codecsOf(t, env, cam.ID); got.live != "h264" {
		t.Fatalf("an idle stream erased its codec: %+v", got)
	}
}

func TestHealthPollerKeepsCodecsWhenGo2RTCIsUnavailable(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"]
	live, hq := streamsOf(t, env, cam.ID)
	poller := &inventory.HealthPoller{Svc: env.Svc, Interval: time.Second, Concurrency: 2, CapabilityInterval: time.Nanosecond}

	mock.Server.Go2RTCStreams = []string{live, hq}
	mock.Server.Go2RTCCodecs = map[string]string{live: "H265", hq: "H265"}
	poller.PollOnce(ctx)

	mock.Server.Go2RTCStreams = nil // the endpoint now answers 404
	time.Sleep(5 * time.Millisecond)
	poller.PollOnce(ctx)
	if got := codecsOf(t, env, cam.ID); got != (streamCodecs{live: "h265", hq: "h265"}) {
		t.Fatalf("go2rtc down must keep the stored codecs, got %+v", got)
	}
}

func TestSyncStoresCodecsAndKeepsThemWhenAStreamIsIdle(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]
	mock := env.Mocks["frigate-h01"]
	live, hq := streamsOf(t, env, cam.ID)

	mock.Server.Go2RTCStreams = []string{live, hq}
	mock.Server.Go2RTCCodecs = map[string]string{live: "H265", hq: "H264"}
	if _, err := env.Svc.SyncServer(ctx, env.Admin, cam.ServerID); err != nil {
		t.Fatal(err)
	}
	if got := codecsOf(t, env, cam.ID); got != (streamCodecs{live: "h265", hq: "h264"}) {
		t.Fatalf("after sync: %+v", got)
	}

	// The live stream has no producer at the next sync: its codec is kept.
	mock.Server.Go2RTCCodecs = map[string]string{hq: "H265"}
	if _, err := env.Svc.SyncServer(ctx, env.Admin, cam.ServerID); err != nil {
		t.Fatal(err)
	}
	if got := codecsOf(t, env, cam.ID); got != (streamCodecs{live: "h265", hq: "h265"}) {
		t.Fatalf("idle stream must keep its codec: %+v", got)
	}

	// The camera loses its go2rtc restream altogether: nothing is left to describe.
	mock.Server.Go2RTCStreams = []string{"another_camera"}
	mock.Server.Go2RTCCodecs = nil
	if _, err := env.Svc.SyncServer(ctx, env.Admin, cam.ServerID); err != nil {
		t.Fatal(err)
	}
	if got := codecsOf(t, env, cam.ID); got != (streamCodecs{}) {
		t.Fatalf("a camera without restream has no codec: %+v", got)
	}
}
