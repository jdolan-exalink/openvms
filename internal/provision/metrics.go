package provision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
)

func fetchMetrics(ctx context.Context, host string, port int32, token string) (agent.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+formatHost(host, port)+"/v1/metrics", nil)
	if err != nil {
		return agent.Snapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return doMetrics(req)
}

// ErrAgentUpdatePending means the agent refused an update because an earlier one has not been
// confirmed yet; it resolves itself once that one is confirmed or rolled back.
var ErrAgentUpdatePending = errors.New("the agent is still waiting to confirm an earlier update")

// updateRequestHeaders are what an agent 0.4 verifies before installing a binary: the checksum
// OpenVMS computed and the version it expects the binary to report. Older agents ignore them.
func updateRequestHeaders(token string, binary []byte, version string) http.Header {
	sum := sha256.Sum256(binary)
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-OpenVMS-SHA256", hex.EncodeToString(sum[:]))
	if version != "" {
		h.Set("X-OpenVMS-Agent-Version", version)
	}
	return h
}

// pushUpdateVia uploads the binary through an agent client (a stored base URL, a VPN, a tunnel).
func pushUpdateVia(ctx context.Context, client AgentProbeDoer, token string, binary []byte, version string) error {
	resp, err := client.Do(ctx, http.MethodPost, "/v1/update", updateRequestHeaders(token, binary, version), bytes.NewReader(binary))
	if err != nil {
		return errors.Join(ErrAgentProbeUnavailable, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusAccepted, http.StatusOK:
		return nil
	case http.StatusConflict:
		return ErrAgentUpdatePending
	default:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &AgentStatusError{Status: resp.StatusCode, Message: stringsTrim(b)}
	}
}

func pushUpdate(ctx context.Context, host string, port int32, token string, binary []byte, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+formatHost(host, port)+"/v1/update", bytes.NewReader(binary))
	if err != nil {
		return err
	}
	req.Header = updateRequestHeaders(token, binary, version)
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return ErrAgentUpdatePending
	}
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("agent update returned %d: %s", resp.StatusCode, stringsTrim(body))
	}
	return nil
}

func doMetrics(req *http.Request) (agent.Snapshot, error) {
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return agent.Snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return agent.Snapshot{}, fmt.Errorf("agent returned %d", resp.StatusCode)
	}
	var snap agent.Snapshot
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&snap); err != nil {
		return agent.Snapshot{}, err
	}
	return snap, nil
}

func stringsTrim(b []byte) string {
	s := string(bytes.TrimSpace(b))
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
