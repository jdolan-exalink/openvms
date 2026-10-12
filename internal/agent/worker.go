package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agent/mtls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
)

// heartbeatTimeout bounds one Heartbeat call.
const heartbeatTimeout = 5 * time.Second

// oldConnGrace is how long a connection replaced after a certificate renewal stays open. A call
// that started on it can run for as long as the longest call the worker makes (the renewal RPC
// or a heartbeat), so the old connection outlives that by a margin instead of cutting it off.
const oldConnGrace = max(mtls.RenewTimeout, heartbeatTimeout) + 10*time.Second

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

	// NewTicker supplies the heartbeat ticks; nil uses a time.Ticker. Tests inject a channel they
	// drive by hand, so heartbeat timing is deterministic. stop is called when Run returns.
	NewTicker func(d time.Duration) (ticks <-chan time.Time, stop func())
	// OnHeartbeat, when set, is called after every heartbeat attempt (the initial one included)
	// with its error, nil when the control server acknowledged it.
	OnHeartbeat func(err error)
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
	opts := grpctls.ClientOptions{CAFile: w.cfg.TLSCAFile, ServerName: w.cfg.TLSServerName}
	if w.cfg.MTLS != nil {
		opts.GetClientCertificate = w.cfg.MTLS.GetClientCertificate
	}
	creds, err := grpctls.TransportCredentials(w.cfg.TLS || w.cfg.MTLS != nil, opts)
	if err != nil {
		return nil, fmt.Errorf("control server TLS: %w", err)
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

	ticks, stopTicker := w.cfg.ticker()
	defer stopTicker()

	// Initial immediate heartbeat
	if err := w.heartbeat(ctx); err != nil {
		w.cfg.Log.Warn("initial heartbeat failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			w.cfg.Log.Info("node agent worker stopping")
			return ctx.Err()
		case <-ticks:
			if w.cfg.MTLS != nil && w.cfg.MTLS.Generation() != generation {
				generation = w.reconnect(generation)
			}
			if err := w.heartbeat(ctx); err != nil {
				w.cfg.Log.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

// ticker returns the heartbeat tick source and its stop function.
func (c WorkerConfig) ticker() (<-chan time.Time, func()) {
	if c.NewTicker != nil {
		return c.NewTicker(c.HeartbeatInterval)
	}
	t := time.NewTicker(c.HeartbeatInterval)
	return t.C, t.Stop
}

// heartbeat sends one heartbeat and reports its outcome to OnHeartbeat.
func (w *Worker) heartbeat(ctx context.Context) error {
	err := w.sendHeartbeat(ctx)
	if w.cfg.OnHeartbeat != nil {
		w.cfg.OnHeartbeat(err)
	}
	return err
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
	time.AfterFunc(oldConnGrace, func() { _ = old.Close() })
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
		RamTotalBytes:     signedBytes(snap.MemoryTotal),
		RamUsedBytes:      usedBytes(snap.MemoryTotal, snap.MemoryAvailable),
		GpuName:           snap.GPUName,
		StorageTotalBytes: signedBytes(snap.CCTVTotal),
		StorageUsedBytes:  usedBytes(snap.CCTVTotal, snap.CCTVFree),
	}

	req := &openvmsv1.NodeHeartbeatRequest{
		NodeId:     w.cfg.NodeID,
		Version:    Version,
		Status:     openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE,
		Specs:      specs,
		Interfaces: pbIfaces,
		Timestamp:  time.Now().Unix(),
	}

	hctx, cancel := context.WithTimeout(ctx, heartbeatTimeout)
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

func signedBytes(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

func usedBytes(total, free uint64) int64 {
	if free > total {
		return 0
	}
	return signedBytes(total - free)
}
