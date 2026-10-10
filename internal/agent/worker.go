package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
)

// WorkerConfig defines registration and telemetry parameters for the Node Agent.
type WorkerConfig struct {
	NodeID            string
	ControlGRPCAddr   string
	HeartbeatInterval time.Duration
	Sampler           *Sampler
	Log               *slog.Logger
	// TLS dials the control server over TLS, verifying its certificate. Plaintext otherwise.
	TLS bool
	// TLSCAFile is a PEM bundle trusted for the server certificate; empty uses system roots.
	TLSCAFile string
	// TLSServerName overrides the name verified against the server certificate.
	TLSServerName string
}

// Worker periodically transmits telemetry and heartbeats to the central OpenVMS Control Server.
type Worker struct {
	cfg        WorkerConfig
	nodeClient openvmsv1.NodeServiceClient
	conn       *grpc.ClientConn
}

// NewWorker initializes a node agent background worker.
func NewWorker(cfg WorkerConfig) *Worker {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 10 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Sampler == nil {
		cfg.Sampler = NewSampler(Paths{})
	}

	return &Worker{
		cfg: cfg,
	}
}

// Run executes the heartbeat loop until ctx is canceled.
func (w *Worker) Run(ctx context.Context) error {
	w.cfg.Log.Info("starting node agent heartbeat worker",
		"node_id", w.cfg.NodeID,
		"control_addr", w.cfg.ControlGRPCAddr,
	)

	creds := credentials.TransportCredentials(insecure.NewCredentials())
	if w.cfg.TLS {
		var err error
		creds, err = grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: w.cfg.TLSCAFile, ServerName: w.cfg.TLSServerName})
		if err != nil {
			return fmt.Errorf("control server TLS: %w", err)
		}
	}

	// Dial control plane with automatic reconnection
	conn, err := grpc.NewClient(
		w.cfg.ControlGRPCAddr,
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		return fmt.Errorf("failed to dial control server: %w", err)
	}
	w.conn = conn
	defer conn.Close()

	w.nodeClient = openvmsv1.NewNodeServiceClient(conn)

	ticker := time.NewTicker(w.cfg.HeartbeatInterval)
	defer ticker.Stop()

	// Initial immediate heartbeat
	if err := w.sendHeartbeat(ctx); err != nil {
		w.cfg.Log.Warn("initial heartbeat failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			w.cfg.Log.Info("node agent worker stopping")
			return ctx.Err()
		case <-ticker.C:
			if err := w.sendHeartbeat(ctx); err != nil {
				w.cfg.Log.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

func (w *Worker) sendHeartbeat(ctx context.Context) error {
	snap := w.cfg.Sampler.Current("")
	ifaces, err := DiscoverInterfaces()
	if err != nil {
		w.cfg.Log.Warn("failed to discover network interfaces", "error", err)
	}

	pbIfaces := make([]*openvmsv1.NodeNetworkInterface, 0, len(ifaces))
	for _, iface := range ifaces {
		pbIfaces = append(pbIfaces, &openvmsv1.NodeNetworkInterface{
			Name:       iface.Name,
			IpAddress:  iface.IPAddress,
			MacAddress: iface.MAC,
			IsDefault:  iface.IsDefault,
		})
	}

	var cpuPct float32
	if snap.CPUPercent != nil {
		cpuPct = float32(*snap.CPUPercent)
	}
	specs := &openvmsv1.NodeSpecs{
		CpuUsagePct:       cpuPct,
		RamTotalBytes:     int64(snap.MemoryTotal),
		RamUsedBytes:      int64(snap.MemoryTotal - snap.MemoryAvailable),
		GpuName:           snap.GPUName,
		StorageTotalBytes: int64(snap.CCTVTotal),
		StorageUsedBytes:  int64(snap.CCTVTotal - snap.CCTVFree),
	}

	req := &openvmsv1.NodeHeartbeatRequest{
		NodeId:     w.cfg.NodeID,
		Version:    Version,
		Status:     openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE,
		Specs:      specs,
		Interfaces: pbIfaces,
		Timestamp:  time.Now().Unix(),
	}

	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := w.nodeClient.Heartbeat(hctx, req)
	if err != nil {
		return err
	}

	if resp.Command != "" {
		w.cfg.Log.Info("received command from control server", "command", resp.Command)
	}

	return nil
}
