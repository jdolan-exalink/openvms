package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

const (
	maxOnvifSnapshotResponseBytes = 8 << 20
	onvifSnapshotTimeout          = 10 * time.Second
)

// ExecuteOnvifSnapshot asks the selected server's edge agent to retrieve one ONVIF frame.
// The camera-provided URI remains on the edge agent and never reaches the API or browser.
func (s *Service) ExecuteOnvifSnapshot(ctx context.Context, actor authz.Actor, serverID uuid.UUID, req onvifdiscover.SnapshotRequest) ([]byte, error) {
	if s.requireServerManage != nil {
		if err := s.requireServerManage(ctx, actor, serverID); err != nil {
			return nil, err
		}
		if err := s.requireProbeConfigSecrets(ctx, actor, serverID); err != nil {
			return nil, err
		}
	} else {
		if s.Inv == nil {
			return nil, ErrAgentProbeFailed
		}
		if err := s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID); err != nil {
			return nil, err
		}
		if req.Username == "" {
			if user, pass, err := s.Inv.GetServerCredentials(ctx, actor, serverID); err == nil && user != "" {
				req.Username, req.Password = user, pass
			}
		}
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(req)
	if err != nil || len(requestBody) > maxOnvifProbeRequestBytes || token == "" {
		return nil, ErrAgentProbeFailed
	}
	ctx, cancel := context.WithTimeout(ctx, onvifSnapshotTimeout)
	defer cancel()
	// #nosec G117 -- camera credentials go only to the authenticated server agent and are never logged or stored.
	response, err := client.Do(ctx, http.MethodPost, "/v1/onvif/snapshot", http.Header{
		"Authorization": []string{"Bearer " + token},
		"Content-Type":  []string{"application/json"},
	}, bytes.NewReader(requestBody))
	if err != nil {
		return nil, ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrAgentProbeFailed
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "image/jpeg") || response.ContentLength > maxOnvifSnapshotResponseBytes {
		return nil, ErrAgentProbeFailed
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxOnvifSnapshotResponseBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxOnvifSnapshotResponseBytes {
		return nil, ErrAgentProbeFailed
	}
	if len(data) < 4 || !bytes.HasPrefix(data, []byte{0xff, 0xd8}) || !bytes.HasSuffix(data, []byte{0xff, 0xd9}) {
		return nil, errors.New("edge agent returned malformed JPEG")
	}
	return data, nil
}
