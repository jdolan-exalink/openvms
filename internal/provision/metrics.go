package provision

import (
	"bytes"
	"context"
	"encoding/json"
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

func pushUpdate(ctx context.Context, host string, port int32, token string, binary []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+formatHost(host, port)+"/v1/update", bytes.NewReader(binary))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
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
