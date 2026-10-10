package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agent/mtls"
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
	// MTLS, when set, dials the agent listener with the managed client certificate. TLS is
	// always on then (TLSCAFile and TLSServerName still select how the server is verified),
	// the node id is the server id in the certificate, and the worker renews the certificate
	// and reconnects with the new one. Nil keeps the unauthenticated behavior.
	MTLS *mtls.Manager
}

// Worker periodically transmits telemetry and heartbeats to the central OpenVMS Control Server.
type Worker struct {
	cfg        WorkerConfig
	nodeClient openvmsv1.NodeServiceClient
	conn       *swapConn
	acked      atomic.Int64
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
	if cfg.MTLS != nil {
		// The listener accepts only the server id the certificate was issued for.
		cfg.NodeID = cfg.MTLS.Current().Identity.ServerID.String()
	}

	return &Worker{
		cfg: cfg,
	}
}

// HeartbeatsAcknowledged is how many heartbeats the control server has acknowledged.
func (w *Worker) HeartbeatsAcknowledged() int64 { return w.acked.Load() }

func (w *Worker) dial() (*grpc.ClientConn, error) {
	creds := credentials.TransportCredentials(insecure.NewCredentials())
	if w.cfg.TLS || w.cfg.MTLS != nil {
		opts := grpctls.ClientOptions{CAFile: w.cfg.TLSCAFile, ServerName: w.cfg.TLSServerName}
		if w.cfg.MTLS != nil {
			opts.GetClientCertificate = w.cfg.MTLS.GetClientCertificate
		}
		var err error
		creds, err = grpctls.ClientCredentials(opts)
		if err != nil {
			return nil, fmt.Errorf("control server TLS: %w", err)
		}
	}
	// grpc.NewClient reconnects automatically.
	conn, err := grpc.NewClient(w.cfg.ControlGRPCAddr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to dial control server: %w", err)
	}
	return conn, nil
}

// Run executes the heartbeat loop until ctx is canceled.
func (w *Worker) Run(ctx context.Context) error {
	w.cfg.Log.Info("starting node agent heartbeat worker",
		"node_id", w.cfg.NodeID,
		"control_addr", w.cfg.ControlGRPCAddr,
		"mtls", w.cfg.MTLS != nil,
	)

	conn, err := w.dial()
	if err != nil {
		return err
	}
	w.conn = &swapConn{}
	w.conn.p.Store(conn)
	defer func() { _ = w.conn.p.Load().Close() }()

	w.nodeClient = openvmsv1.NewNodeServiceClient(w.conn)

	// Under mTLS the certificate is renewed here, over the same authenticated channel, and
	// connections are re-established with the renewed certificate.
	generation := uint64(0)
	if w.cfg.MTLS != nil {
		generation = w.cfg.MTLS.Generation()
		renewCtx, stopRenewal := context.WithCancel(ctx)
		defer stopRenewal()
		go w.cfg.MTLS.Run(renewCtx, mtls.GRPCRenew(w.conn))
	}

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
			if w.cfg.MTLS != nil && w.cfg.MTLS.Generation() != generation {
				generation = w.reconnect(generation)
			}
			if err := w.sendHeartbeat(ctx); err != nil {
				w.cfg.Log.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

// reconnect replaces the connection with one that handshakes with the renewed certificate and
// returns the credentials generation it now uses. The old connection is closed after a grace
// period so a call in flight on it can finish. If the dial fails the old connection stays, and
// the next tick tries again.
func (w *Worker) reconnect(current uint64) uint64 {
	next := w.cfg.MTLS.Generation()
	conn, err := w.dial()
	if err != nil {
		w.cfg.Log.Warn("reconnect with the renewed certificate failed", "error", err)
		return current
	}
	old := w.conn.p.Swap(conn)
	w.cfg.Log.Info("reconnected with the renewed agent certificate")
	time.AfterFunc(5*time.Second, func() { _ = old.Close() })
	return next
}

// swapConn lets the heartbeat and renewal clients keep one handle while the underlying
// connection is replaced after a certificate renewal.
type swapConn struct {
	p atomic.Pointer[grpc.ClientConn]
}

func (s *swapConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	return s.p.Load().Invoke(ctx, method, args, reply, opts...)
}

func (s *swapConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return s.p.Load().NewStream(ctx, desc, method, opts...)
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

	if resp.Acknowledged {
		w.acked.Add(1)
	}
	if resp.Command != "" {
		w.cfg.Log.Info("received command from control server", "command", resp.Command)
	}

	return nil
}
