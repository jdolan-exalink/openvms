package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

// ErrAgentBackupUnsupported means the agent has no original-configuration backup routes: it is older
// than the release that added them.
var ErrAgentBackupUnsupported = errors.New("the agent does not support configuration backups; update the agent to enable backups")

const (
	agentBackupPath = "/v1/backup/original"
	// The agent answers a read with the whole config.yml (at most 2 MiB, more once JSON escapes it).
	maxAgentBackupResponseBytes = 6 << 20
	agentBackupTimeout          = 20 * time.Second
	// How long a best-effort backup triggered by another operation may take in the background.
	agentBackupBackgroundTimeout = 2 * time.Minute
	maxRegisteredByRunes         = 200
)

// AgentBackup is what the agent says about the original Frigate configuration it keeps. It never
// carries the configuration itself.
type AgentBackup struct {
	Exists         bool
	SavedAt        time.Time
	FrigateVersion string
	RegisteredBy   string
	SHA256         string
	Size           int
}

// backupWire is the metadata in the agent's answers. The config field of a read is deliberately not
// part of it, so the text is never kept.
type backupWire struct {
	Code           string `json:"code"`
	SavedAt        string `json:"saved_at"`
	FrigateVersion string `json:"frigate_version"`
	RegisteredBy   string `json:"registered_by"`
	SHA256         string `json:"sha256"`
	Size           int    `json:"size"`
}

func (w backupWire) backup() AgentBackup {
	out := AgentBackup{Exists: true, FrigateVersion: w.FrigateVersion, RegisteredBy: w.RegisteredBy, SHA256: w.SHA256, Size: w.Size}
	if t, err := time.Parse(time.RFC3339, w.SavedAt); err == nil {
		out.SavedAt = t.UTC()
	}
	return out
}

// backupSources are the central lookups the ensure flow needs. The production ones come from the
// inventory service; tests give fakes.
type backupSources struct {
	original func(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (yaml, frigateVersion string, err error)
	audit    func(ctx context.Context, actor authz.Actor, serverID uuid.UUID, sha256 string) error
}

func (s *Service) backupSources() (backupSources, error) {
	if s.Inv == nil {
		return backupSources{}, errors.New("server backup unavailable")
	}
	return backupSources{original: s.Inv.OriginalFrigateConfig, audit: s.Inv.AuditAgentBackupSaved}, nil
}

// GetAgentBackup reports whether the agent holds the original configuration, with its metadata. It
// never returns the configuration and never creates anything. Requires servers.manage.
func (s *Service) GetAgentBackup(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentBackup, error) {
	if err := s.requireBackupManage(ctx, actor, serverID); err != nil {
		return AgentBackup{}, err
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return AgentBackup{}, err
	}
	return readAgentBackup(ctx, client, token)
}

// EnsureOriginalConfigBackup makes the agent keep the original Frigate configuration of the server:
// when it already has one nothing is sent; otherwise the original (see
// inventory.OriginalFrigateConfig) is stored. The agent keeps the first copy it receives, so calling
// this again, or racing with another OpenVMS, never replaces what is stored. Requires servers.manage
// and servers.config.secrets, because the file holds camera passwords.
func (s *Service) EnsureOriginalConfigBackup(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (AgentBackup, error) {
	sources, err := s.backupSources()
	if err != nil {
		return AgentBackup{}, err
	}
	return s.ensureOriginalConfigBackup(ctx, actor, serverID, sources)
}

func (s *Service) ensureOriginalConfigBackup(ctx context.Context, actor authz.Actor, serverID uuid.UUID, sources backupSources) (AgentBackup, error) {
	if err := s.requireBackupManageAndSecrets(ctx, actor, serverID); err != nil {
		return AgentBackup{}, err
	}
	client, token, err := s.getAgentClientAndToken(ctx, actor, serverID)
	if err != nil {
		return AgentBackup{}, err
	}
	existing, err := readAgentBackup(ctx, client, token)
	if err != nil {
		return AgentBackup{}, err
	}
	if existing.Exists {
		return existing, nil
	}

	original, version, err := sources.original(ctx, actor, serverID)
	if err != nil {
		return AgentBackup{}, err
	}
	body, err := json.Marshal(map[string]string{
		"config": original, "frigate_version": version, "registered_by": backupRegisteredBy(actor),
	})
	if err != nil {
		return AgentBackup{}, ErrAgentProbeFailed
	}
	status, data, err := agentBackupRequest(ctx, client, token, http.MethodPut, body)
	if err != nil {
		return AgentBackup{}, err
	}
	switch status {
	case http.StatusCreated:
		saved, err := parseBackupWire(data)
		if err != nil {
			return AgentBackup{}, err
		}
		if err := sources.audit(ctx, actor, serverID, saved.SHA256); err != nil {
			// The backup is stored; failing to audit it must not hide that.
			s.logger().Warn("agent backup: audit entry not written", "server_id", serverID, "error", err)
		}
		return saved, nil
	case http.StatusConflict:
		// Another OpenVMS, or another request, stored it first: that copy stays.
		return parseBackupWire(data)
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return AgentBackup{}, ErrAgentBackupUnsupported
	default:
		return AgentBackup{}, fmt.Errorf("%w: the agent answered %d to the backup", ErrAgentProbeFailed, status)
	}
}

// readAgentBackup asks the agent for its backup and keeps only the metadata.
func readAgentBackup(ctx context.Context, client AgentProbeDoer, token string) (AgentBackup, error) {
	status, data, err := agentBackupRequest(ctx, client, token, http.MethodGet, nil)
	if err != nil {
		return AgentBackup{}, err
	}
	switch status {
	case http.StatusOK:
		return parseBackupWire(data)
	case http.StatusNotFound:
		var wire backupWire
		if json.Unmarshal(data, &wire) == nil && wire.Code == "no_backup" {
			return AgentBackup{}, nil
		}
		// A 404 without that code is the agent not knowing the route at all.
		return AgentBackup{}, ErrAgentBackupUnsupported
	case http.StatusMethodNotAllowed:
		return AgentBackup{}, ErrAgentBackupUnsupported
	default:
		return AgentBackup{}, fmt.Errorf("%w: the agent answered %d to the backup read", ErrAgentProbeFailed, status)
	}
}

func parseBackupWire(data []byte) (AgentBackup, error) {
	var wire backupWire
	if err := json.Unmarshal(data, &wire); err != nil || wire.SHA256 == "" {
		return AgentBackup{}, fmt.Errorf("%w: the agent's backup answer is not valid", ErrAgentProbeFailed)
	}
	return wire.backup(), nil
}

// agentBackupRequest sends one request to the backup route. The body of an error answer is returned
// to the caller only to read the code; it is never logged or put in an error.
func agentBackupRequest(ctx context.Context, client AgentProbeDoer, token, method string, body []byte) (int, []byte, error) {
	if token == "" {
		return 0, nil, ErrAgentProbeFailed
	}
	ctx, cancel := context.WithTimeout(ctx, agentBackupTimeout)
	defer cancel()
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
		headers.Set("Content-Type", "application/json")
	}
	response, err := client.Do(ctx, method, agentBackupPath, headers, payload)
	if err != nil {
		return 0, nil, ErrAgentProbeUnavailable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAgentBackupResponseBytes+1))
	if err != nil || len(data) > maxAgentBackupResponseBytes {
		return 0, nil, ErrAgentProbeFailed
	}
	return response.StatusCode, data, nil
}

// backupRegisteredBy is the free text stored with the backup so whoever finds the server later knows
// which OpenVMS registered it.
func backupRegisteredBy(actor authz.Actor) string {
	text := "OpenVMS"
	if actor.Username != "" {
		text += " (saved by " + actor.Username + ")"
	}
	if utf8.RuneCountInString(text) > maxRegisteredByRunes {
		text = string([]rune(text)[:maxRegisteredByRunes])
	}
	return text
}

func (s *Service) requireBackupManage(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if s.requireServerManage != nil {
		return s.requireServerManage(ctx, actor, serverID)
	}
	if s.Inv == nil {
		return errors.New("server backup authorization unavailable")
	}
	_, err := s.Inv.RequireServerManage(ctx, actor, serverID)
	return err
}

func (s *Service) requireBackupManageAndSecrets(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error {
	if s.requireServerManage != nil {
		if err := s.requireServerManage(ctx, actor, serverID); err != nil {
			return err
		}
		return s.requireProbeConfigSecrets(ctx, actor, serverID)
	}
	if s.Inv == nil {
		return errors.New("server backup authorization unavailable")
	}
	return s.Inv.RequireServerManageAndConfigSecrets(ctx, actor, serverID)
}

// ensureBackupInBackground stores the original configuration on the host after an operation that
// reached the agent (connect, install, update). It is best effort: it never delays or fails that
// operation, runs under its own bounded context and only logs a warning when it cannot finish. It
// reuses the actor that started the operation.
func (s *Service) ensureBackupInBackground(ctx context.Context, actor authz.Actor, serverID uuid.UUID, trigger string) {
	if s.Inv == nil {
		return
	}
	// #nosec G118 -- the backup outlives the request that triggered it by design.
	go func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), agentBackupBackgroundTimeout)
		defer cancel()
		if _, err := s.EnsureOriginalConfigBackup(bg, actor, serverID); err != nil {
			s.logger().Warn("agent backup: the original configuration was not stored", "server_id", serverID, "trigger", trigger, "error", err)
		}
	}()
}
