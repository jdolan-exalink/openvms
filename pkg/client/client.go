package client

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
)

// Config configures the OpenVMS Desktop Client.
type Config struct {
	ServerAddress string
	DeviceID      string
	DeviceName    string
	Platform      string
	Architecture  string
	AppVersion    string
	Timeout       time.Duration

	// TLS dials the control plane over TLS and verifies the server certificate.
	// Plaintext is the default.
	TLS bool
	// TLSCAFile is a PEM bundle trusted for the server certificate; empty uses system roots.
	TLSCAFile string
	// TLSServerName overrides the name verified against the server certificate.
	TLSServerName string
}

// Client provides enterprise connectivity for OpenVMS Desktop applications.
type Client struct {
	cfg       Config
	conn      *grpc.ClientConn
	authToken string
	mu        sync.RWMutex

	authClient  openvmsv1.AuthServiceClient
	siteClient  openvmsv1.SiteServiceClient
	nodeClient  openvmsv1.NodeServiceClient
	camClient   openvmsv1.CameraServiceClient
	connClient  openvmsv1.ConnectionServiceClient
	eventClient openvmsv1.EventServiceClient
}

// New creates and connects an OpenVMS Desktop Client.
func New(cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Platform == "" {
		cfg.Platform = "linux"
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = "1.0.0"
	}

	creds := credentials.TransportCredentials(insecure.NewCredentials())
	if cfg.TLS {
		var err error
		creds, err = grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: cfg.TLSCAFile, ServerName: cfg.TLSServerName})
		if err != nil {
			return nil, fmt.Errorf("control plane TLS: %w", err)
		}
	} else if cfg.TLSCAFile != "" || cfg.TLSServerName != "" {
		return nil, fmt.Errorf("TLSCAFile and TLSServerName require TLS to be enabled")
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
	}

	conn, err := grpc.NewClient(cfg.ServerAddress, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial control plane: %w", err)
	}

	c := &Client{
		cfg:         cfg,
		conn:        conn,
		authClient:  openvmsv1.NewAuthServiceClient(conn),
		siteClient:  openvmsv1.NewSiteServiceClient(conn),
		nodeClient:  openvmsv1.NewNodeServiceClient(conn),
		camClient:   openvmsv1.NewCameraServiceClient(conn),
		connClient:  openvmsv1.NewConnectionServiceClient(conn),
		eventClient: openvmsv1.NewEventServiceClient(conn),
	}

	return c, nil
}

// Close disconnects the client.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) withAuth(ctx context.Context) context.Context {
	c.mu.RLock()
	token := c.authToken
	c.mu.RUnlock()

	if token != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
	return ctx
}

// Handshake performs protocol and feature negotiation with the server.
func (c *Client) Handshake(ctx context.Context) (*openvmsv1.HandshakeResponse, error) {
	return c.authClient.Handshake(ctx, &openvmsv1.HandshakeRequest{
		Client: &openvmsv1.ClientInfo{
			DeviceId:     c.cfg.DeviceID,
			Name:         c.cfg.DeviceName,
			Platform:     c.cfg.Platform,
			Architecture: c.cfg.Architecture,
			Version:      c.cfg.AppVersion,
		},
	})
}

// Login authenticates user credentials and stores the session access token.
func (c *Client) Login(ctx context.Context, username, password, mfaCode string) (*openvmsv1.LoginResponse, error) {
	resp, err := c.authClient.Login(ctx, &openvmsv1.LoginRequest{
		Username: username,
		Password: password,
		MfaCode:  mfaCode,
		DeviceId: c.cfg.DeviceID,
	})
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.authToken = resp.AccessToken
	c.mu.Unlock()

	return resp, nil
}

// ListSites discovers available physical sites.
func (c *Client) ListSites(ctx context.Context) ([]*openvmsv1.Site, error) {
	resp, err := c.siteClient.ListSites(c.withAuth(ctx), &openvmsv1.ListSitesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Sites, nil
}

// ListNodes discovers NVR/Frigate nodes in a site.
func (c *Client) ListNodes(ctx context.Context, siteID string) ([]*openvmsv1.Node, error) {
	resp, err := c.nodeClient.ListNodes(c.withAuth(ctx), &openvmsv1.ListNodesRequest{
		SiteId: siteID,
	})
	if err != nil {
		return nil, err
	}
	return resp.Nodes, nil
}

// ListCameras discovers cameras across sites or nodes.
func (c *Client) ListCameras(ctx context.Context, siteID, nodeID string) ([]*openvmsv1.Camera, error) {
	resp, err := c.camClient.ListCameras(c.withAuth(ctx), &openvmsv1.ListCamerasRequest{
		SiteId: siteID,
		NodeId: nodeID,
	})
	if err != nil {
		return nil, err
	}
	return resp.Cameras, nil
}

// GetCameraSnapshot downloads an instant frame from a camera.
func (c *Client) GetCameraSnapshot(ctx context.Context, cameraID string, height int32) ([]byte, string, error) {
	resp, err := c.camClient.GetCameraSnapshot(c.withAuth(ctx), &openvmsv1.GetCameraSnapshotRequest{
		CameraId: cameraID,
		Height:   height,
	})
	if err != nil {
		return nil, "", err
	}
	return resp.ImageData, resp.ContentType, nil
}

// GetConnectionCandidates retrieves connection route candidates for a camera or node.
func (c *Client) GetConnectionCandidates(ctx context.Context, nodeID, cameraID string) (*openvmsv1.GetConnectionCandidatesResponse, error) {
	return c.connClient.GetConnectionCandidates(c.withAuth(ctx), &openvmsv1.GetConnectionCandidatesRequest{
		NodeId:         nodeID,
		CameraId:       cameraID,
		ClientDeviceId: c.cfg.DeviceID,
	})
}

// ProbeBestCandidate tests candidate reachability in parallel and selects the optimal path.
func (c *Client) ProbeBestCandidate(ctx context.Context, candidates []*openvmsv1.ConnectionCandidate) (*openvmsv1.ConnectionCandidate, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidates provided")
	}

	type probeResult struct {
		candidate *openvmsv1.ConnectionCandidate
		rtt       time.Duration
		err       error
	}

	ch := make(chan probeResult, len(candidates))
	for _, cand := range candidates {
		go func(cd *openvmsv1.ConnectionCandidate) {
			addr := fmt.Sprintf("%s:%d", cd.Address, cd.Port)
			start := time.Now()
			d := net.Dialer{Timeout: 1500 * time.Millisecond}
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				ch <- probeResult{candidate: cd, err: err}
				return
			}
			conn.Close()
			ch <- probeResult{candidate: cd, rtt: time.Since(start)}
		}(cand)
	}

	var best *openvmsv1.ConnectionCandidate
	var bestScore int32 = -1

	for i := 0; i < len(candidates); i++ {
		res := <-ch
		if res.err == nil {
			// Candidates with higher priority are preferred, RTT breaks ties
			score := res.candidate.Priority
			if score > bestScore {
				bestScore = score
				best = res.candidate
			}
		}
	}

	if best != nil {
		return best, nil
	}

	// Fallback to highest priority even if TCP probe failed (e.g. UDP-only candidate)
	best = candidates[0]
	for _, cd := range candidates {
		if cd.Priority > best.Priority {
			best = cd
		}
	}
	return best, nil
}

// SubscribeEvents opens a persistent gRPC bidirectional stream for real-time events.
func (c *Client) SubscribeEvents(ctx context.Context, sub *openvmsv1.EventSubscription, handler func(*openvmsv1.OpenVMSEvent)) error {
	stream, err := c.eventClient.SubscribeEvents(c.withAuth(ctx))
	if err != nil {
		return fmt.Errorf("failed to open event stream: %w", err)
	}

	if err := stream.Send(sub); err != nil {
		return fmt.Errorf("failed to send subscription filter: %w", err)
	}

	for {
		event, err := stream.Recv()
		if err != nil {
			return err
		}
		if handler != nil {
			handler(event)
		}
	}
}
