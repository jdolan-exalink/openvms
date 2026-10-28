package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Server kinds, as stored in frigate_servers.kind.
const (
	KindFrigate      = "frigate"
	KindProvisionNVR = "provision_nvr"
)

// UnsupportedKindError reports an operation that only exists for another kind of server, for example a
// Frigate configuration change asked of a Provision-ISR recorder.
type UnsupportedKindError struct{ Kind string }

func (e *UnsupportedKindError) Error() string {
	return "this operation is not available for " + e.Kind + " servers"
}

// requireFrigate fails for a server that is not a Frigate server. An empty kind is a Frigate server: it
// comes from a partial row built before kinds existed, never from the database.
func requireFrigate(kind string) error {
	if kind != "" && kind != KindFrigate {
		return &UnsupportedKindError{Kind: kind}
	}
	return nil
}

// NVRError is a failed call to a Provision-ISR recorder. Class says what the caller should tell the
// user; the underlying error stays out of the message because it may describe the recorder's answer.
type NVRError struct {
	Class provisionnvr.Class
	Err   error
}

func (e *NVRError) Error() string { return "provision NVR: " + string(e.Class) }
func (e *NVRError) Unwrap() error { return e.Err }

// NVRConnInput is how a recorder is reached. Certificates are always verified when HTTPS is used.
type NVRConnInput struct {
	Host     string
	Port     int
	HTTPS    bool
	Username string
	Password string
}

func (in NVRConnInput) validate() error {
	host := strings.TrimSpace(in.Host)
	if host == "" || strings.ContainsAny(host, "/\\?#@ \t\r\n") || strings.Contains(host, "://") {
		return invalid("host must be a host name or IP address, without scheme, port or path")
	}
	if strings.Count(host, ":") == 1 {
		return invalid("host must not include a port; use the port field")
	}
	if in.Port < 0 || in.Port > 65535 {
		return invalid("port must be between 1 and 65535")
	}
	if in.Username == "" || in.Password == "" {
		return invalid("username and password are required")
	}
	return nil
}

func (in NVRConnInput) baseURL() string {
	port := in.Port
	scheme := "http"
	if in.HTTPS {
		scheme = "https"
	}
	if port == 0 {
		port = 80
		if in.HTTPS {
			port = 443
		}
	}
	host := strings.Trim(strings.TrimSpace(in.Host), "[]")
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// detectNVR reads a recorder and maps its failures to errors the API can answer with.
func detectNVR(ctx context.Context, baseURL, username, password string) (*provisionnvr.Detection, error) {
	det, err := provisionnvr.Detect(ctx, provisionnvr.DetectInput{BaseURL: baseURL, Username: username, Password: password})
	if err != nil {
		return nil, mapNVRErr(err)
	}
	return det, nil
}

// mapNVRErr turns a failure of the recorder client into an error the API can answer with.
func mapNVRErr(err error) error {
	if class, ok := provisionnvr.ClassOf(err); ok {
		return &NVRError{Class: class, Err: err}
	}
	if errors.Is(err, provisionnvr.ErrInvalidEndpoint) {
		return invalid("host or port is not valid")
	}
	return &NVRError{Class: provisionnvr.ClassOffline, Err: err}
}

// DetectProvisionNvr reads a recorder's identity, channels, storage and capabilities without saving
// anything. Needs servers.manage on the site. The password is used for this call and not kept.
func (s *Service) DetectProvisionNvr(ctx context.Context, actor authz.Actor, siteID uuid.UUID, conn NVRConnInput) (*provisionnvr.Detection, error) {
	if err := conn.validate(); err != nil {
		return nil, err
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		_, err := requireSite(ctx, q, c, authz.ServersManage, siteID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return detectNVR(ctx, conn.baseURL(), conn.Username, conn.Password)
}

// NVRInfo is what a recorder reported the last time it was read; it lives in the server's capabilities
// column under its own key, so the Frigate capability flags of that column stay untouched.
type NVRInfo struct {
	Model           string `json:"model"`
	HardwareModel   string `json:"hardware_model"`
	Firmware        string `json:"firmware"`
	ChannelCapacity int    `json:"channel_capacity"`
	// RTSPPort is the port of the recorder's RTSP server as it reported it; zero when unknown.
	RTSPPort     int                       `json:"rtsp_port,omitempty"`
	Capabilities []provisionnvr.Capability `json:"capabilities"`
}

const nvrCapabilitiesKey = "provision_nvr"

func nvrInfoOf(d *provisionnvr.Detection) NVRInfo {
	return NVRInfo{Model: d.Device.Model, HardwareModel: d.Device.HardwareModel, Firmware: d.Device.SoftwareVersion,
		ChannelCapacity: d.SystemCaps.IPChannelMax, RTSPPort: d.Services.RTSPPort, Capabilities: d.Capabilities}
}

func nvrCapabilitiesJSON(d *provisionnvr.Detection) ([]byte, error) {
	return json.Marshal(map[string]any{nvrCapabilitiesKey: nvrInfoOf(d)})
}

// Provision returns what the recorder reported, nil for a server that is not a Provision recorder.
func (v ServerView) Provision() *NVRInfo {
	if v.Kind != KindProvisionNVR {
		return nil
	}
	return NVRInfoFromCapabilities(v.Capabilities)
}

// NVRInfoFromCapabilities reads what a recorder reported from a server's capabilities column; an empty
// NVRInfo when nothing was stored.
func NVRInfoFromCapabilities(capabilities []byte) *NVRInfo {
	var raw map[string]json.RawMessage
	if json.Unmarshal(capabilities, &raw) != nil || raw[nvrCapabilitiesKey] == nil {
		return &NVRInfo{}
	}
	var info NVRInfo
	_ = json.Unmarshal(raw[nvrCapabilitiesKey], &info)
	return &info
}

// CreateNVRInput registers a Provision recorder and says which of its channels become cameras.
type CreateNVRInput struct {
	SiteID uuid.UUID
	Name   string
	Conn   NVRConnInput
	// ChannelIDs are the channels to import; empty imports every channel the recorder lists.
	ChannelIDs []string
}

// CreateProvisionNVR registers a Provision-ISR recorder after proving the credentials work, and imports
// the chosen channels as cameras. Needs servers.manage on the site. Nothing is written to the recorder.
func (s *Service) CreateProvisionNVR(ctx context.Context, actor authz.Actor, in CreateNVRInput) (ServerView, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ServerView{}, invalid("name is required")
	}
	if err := in.Conn.validate(); err != nil {
		return ServerView{}, err
	}
	// Authorize before contacting the recorder so an unauthorized caller cannot use the VMS to probe hosts.
	var site db.GetSiteRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		site, err = requireSite(ctx, q, c, authz.ServersManage, in.SiteID)
		return err
	})
	if err != nil {
		return ServerView{}, err
	}
	baseURL := in.Conn.baseURL()
	det, err := detectNVR(ctx, baseURL, in.Conn.Username, in.Conn.Password)
	if err != nil {
		return ServerView{}, err
	}
	channels, err := selectChannels(det, in.ChannelIDs)
	if err != nil {
		return ServerView{}, err
	}
	id := uuid.New()
	sealed, err := s.Sealer.Seal([]byte(in.Conn.Password), id[:])
	if err != nil {
		return ServerView{}, err
	}
	caps, err := nvrCapabilitiesJSON(det)
	if err != nil {
		return ServerView{}, err
	}
	kind := KindProvisionNVR
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if _, err := requireSite(ctx, q, c, authz.ServersManage, in.SiteID); err != nil {
			return err
		}
		if err := requireFreeAddress(ctx, q, site.TenantID, id, baseURL); err != nil {
			return err
		}
		if err := q.CreateServer(ctx, db.CreateServerParams{
			ID: id, TenantID: site.TenantID, SiteID: site.ID, Name: in.Name, BaseUrl: baseURL,
			AuthMode: "credentials", Username: in.Conn.Username, PasswordSealed: sealed,
			Capabilities: caps, Kind: &kind,
		}); err != nil {
			return store.Classify(err)
		}
		for _, ch := range channels {
			if _, _, err := upsertNVRChannel(ctx, q, site.TenantID, site.ID, id, ch); err != nil {
				return err
			}
		}
		return audit(ctx, q, actor, &site.TenantID, ActionServerAdded, "server", id, map[string]any{
			"name": in.Name, "kind": kind, "base_url": baseURL, "model": det.Device.Model, "cameras_imported": len(channels),
		})
	})
	if err != nil {
		return ServerView{}, err
	}
	return s.GetServer(ctx, actor, id)
}

func selectChannels(det *provisionnvr.Detection, ids []string) ([]provisionnvr.Channel, error) {
	all := make([]provisionnvr.Channel, 0, len(det.Channels))
	byID := map[string]provisionnvr.Channel{}
	for _, c := range det.Channels {
		all = append(all, c.Channel)
		byID[c.ID] = c.Channel
	}
	if len(ids) == 0 {
		return all, nil
	}
	seen := map[string]bool{}
	out := make([]provisionnvr.Channel, 0, len(ids))
	for _, id := range ids {
		ch, ok := byID[id]
		if !ok {
			return nil, invalid("channel %q is not on the recorder", id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, ch)
		}
	}
	return out, nil
}

// upsertNVRChannel stores one recorder channel as a camera: its remote name is the channel id, which
// stays the same when the channel is renamed. Phase 1 has no video, so the stream fields stay empty.
// It reports whether a row was inserted, or skipped because the channel was deleted here (tombstoned).
func upsertNVRChannel(ctx context.Context, q *db.Queries, tenantID, siteID, serverID uuid.UUID, ch provisionnvr.Channel) (inserted, skipped bool, err error) {
	name := strings.TrimSpace(ch.Name)
	if name == "" {
		name = ch.ID
	}
	inserted, err = q.UpsertCamera(ctx, db.UpsertCameraParams{
		TenantID: tenantID, SiteID: siteID, ServerID: serverID, RemoteName: ch.ID, DisplayName: name,
		Enabled: true, Zones: []string{},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, true, nil
	}
	if err != nil || !inserted {
		return inserted, false, err
	}
	return true, false, q.RematerializeDynamicGroups(ctx, db.RematerializeDynamicGroupsParams{TenantID: tenantID})
}

// syncProvisionNVR re-reads a recorder: it refreshes the stored capabilities and the cameras it already
// has, and marks the ones whose channel is gone. It does not add channels (see the plan: adding a channel
// later is an open question), and it writes nothing to the recorder.
func (s *Service) syncProvisionNVR(ctx context.Context, actor authz.Actor, srv db.GetServerRow) (SyncResult, error) {
	pw, err := s.Sealer.Open(srv.PasswordSealed, srv.ID[:])
	if err != nil {
		return SyncResult{}, err
	}
	det, err := detectNVR(ctx, srv.BaseUrl, srv.Username, string(pw))
	if err != nil {
		return SyncResult{}, err
	}
	caps, err := nvrCapabilitiesJSON(det)
	if err != nil {
		return SyncResult{}, err
	}
	onRecorder := map[string]provisionnvr.Channel{}
	present := make([]string, 0, len(det.Channels))
	for _, c := range det.Channels {
		onRecorder[c.ID] = c.Channel
		present = append(present, c.ID)
	}
	var res SyncResult
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		if err := q.UpdateServerDiscovery(ctx, db.UpdateServerDiscoveryParams{ID: srv.ID, FrigateVersion: srv.FrigateVersion, Capabilities: caps}); err != nil {
			return err
		}
		cams, err := q.ListServerCameras(ctx, srv.ID)
		if err != nil {
			return err
		}
		for _, cam := range cams {
			ch, ok := onRecorder[cam.RemoteName]
			if !ok {
				continue
			}
			if _, _, err := upsertNVRChannel(ctx, q, srv.TenantID, srv.SiteID, srv.ID, ch); err != nil {
				return err
			}
			res.Updated++
		}
		missing, err := q.MarkMissingCameras(ctx, db.MarkMissingCamerasParams{ServerID: srv.ID, Present: present})
		if err != nil {
			return err
		}
		res.Missing = int(missing)
		return audit(ctx, q, actor, &srv.TenantID, ActionServerSynced, "server", srv.ID, map[string]any{
			"kind": KindProvisionNVR, "updated": res.Updated, "missing": res.Missing,
		})
	})
	if err != nil {
		return SyncResult{}, err
	}
	res.Server, err = s.GetServer(ctx, actor, srv.ID)
	return res, err
}

// normalizeNVRBase turns what a user typed ("http://host", "https://host:8443") into the stored form
// scheme://host:port, with the default port made explicit like registration does.
func normalizeNVRBase(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", invalid("the address must look like http://host or https://host:port")
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return u.Scheme + "://" + net.JoinHostPort(u.Hostname(), port), nil
}

// updateProvisionNVR renames a recorder, moves it to another site, and changes its connection (address,
// account, password). A changed connection is proven first: the recorder must answer with the new
// settings, and it must still be the same recorder, which means at least one imported channel exists on
// it; otherwise nothing is saved. A blank password keeps the stored one, unless the account changes.
func (s *Service) updateProvisionNVR(ctx context.Context, actor authz.Actor, current db.GetServerRow, in UpdateServerInput) (ServerView, error) {
	if in.AuthMode != nil || in.TLSSkipVerify != nil || in.EventAlarmMode != nil {
		return ServerView{}, invalid("a Provision recorder has no authentication mode, certificate switch or event alarm mode to edit")
	}
	name, siteID := current.Name, current.SiteID
	if in.Name != nil {
		if name = strings.TrimSpace(*in.Name); name == "" {
			return ServerView{}, invalid("name cannot be empty")
		}
	}
	if in.SiteID != nil {
		siteID = *in.SiteID
	}

	base, user, sealed := current.BaseUrl, current.Username, current.PasswordSealed
	var caps []byte
	connChanged := false
	if in.BaseURL != nil || in.Username != nil || (in.Password != nil && *in.Password != "") {
		var err error
		if in.BaseURL != nil {
			if base, err = normalizeNVRBase(*in.BaseURL); err != nil {
				return ServerView{}, err
			}
		}
		if in.Username != nil {
			if user = strings.TrimSpace(*in.Username); user == "" {
				return ServerView{}, invalid("username cannot be empty")
			}
		}
		newPassword := in.Password != nil && *in.Password != ""
		connChanged = base != current.BaseUrl || user != current.Username || newPassword
		if connChanged {
			if user != current.Username && !newPassword {
				return ServerView{}, invalid("enter the password of the new account")
			}
			var pw string
			if newPassword {
				pw = *in.Password
			} else {
				stored, err := s.Sealer.Open(current.PasswordSealed, current.ID[:])
				if err != nil {
					return ServerView{}, err
				}
				pw = string(stored)
			}
			if base != current.BaseUrl {
				err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
					if err := c.Require(authz.ServersManage, access.Server(current.TenantID, current.SiteID, current.ID)); err != nil {
						return err
					}
					return requireFreeAddress(ctx, q, current.TenantID, current.ID, base)
				})
				if err != nil {
					return ServerView{}, err
				}
			}
			det, err := detectNVR(ctx, base, user, pw)
			if err != nil {
				return ServerView{}, err
			}
			if err := s.requireSameRecorder(ctx, actor, current, det); err != nil {
				return ServerView{}, err
			}
			if caps, err = nvrCapabilitiesJSON(det); err != nil {
				return ServerView{}, err
			}
			if newPassword {
				if sealed, err = s.Sealer.Seal([]byte(pw), current.ID[:]); err != nil {
					return ServerView{}, err
				}
			}
		}
	}

	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.ServersManage, access.Server(current.TenantID, current.SiteID, current.ID)); err != nil {
			return err
		}
		if siteID != current.SiteID {
			site, err := requireSite(ctx, q, c, authz.ServersManage, siteID)
			if err != nil {
				return err
			}
			if site.TenantID != current.TenantID {
				return invalid("a server cannot move to another tenant")
			}
		}
		if base != current.BaseUrl {
			if err := requireFreeAddress(ctx, q, current.TenantID, current.ID, base); err != nil {
				return err
			}
		}
		if err := q.UpdateServer(ctx, db.UpdateServerParams{
			ID: current.ID, Name: name, SiteID: siteID, BaseUrl: base, AuthMode: current.AuthMode, Username: user,
			PasswordSealed: sealed, TlsSkipVerify: current.TlsSkipVerify,
		}); err != nil {
			return store.Classify(err)
		}
		if caps != nil {
			if err := q.UpdateServerDiscovery(ctx, db.UpdateServerDiscoveryParams{ID: current.ID, FrigateVersion: current.FrigateVersion, Capabilities: caps}); err != nil {
				return err
			}
		}
		if siteID != current.SiteID {
			if err := q.MoveServerCameras(ctx, db.MoveServerCamerasParams{ServerID: current.ID, SiteID: siteID}); err != nil {
				return err
			}
			if err := q.RematerializeDynamicGroups(ctx, db.RematerializeDynamicGroupsParams{TenantID: current.TenantID}); err != nil {
				return err
			}
		}
		return audit(ctx, q, actor, &current.TenantID, ActionServerUpdated, "server", current.ID, map[string]any{
			"kind": KindProvisionNVR, "renamed": name != current.Name, "moved": siteID != current.SiteID, "connection_changed": connChanged,
		})
	})
	if err != nil {
		return ServerView{}, err
	}
	return s.GetServer(ctx, actor, current.ID)
}

// requireSameRecorder refuses a new connection that answers as another recorder: a server with cameras
// must find at least one of their channels on it, so the cameras keep pointing at real channels.
func (s *Service) requireSameRecorder(ctx context.Context, actor authz.Actor, srv db.GetServerRow, det *provisionnvr.Detection) error {
	var known []string
	err := s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		cams, err := q.ListServerCameras(ctx, srv.ID)
		for _, c := range cams {
			known = append(known, c.RemoteName)
		}
		return err
	})
	if err != nil {
		return err
	}
	if len(known) == 0 {
		return nil
	}
	onRecorder := map[string]bool{}
	for _, c := range det.Channels {
		onRecorder[c.ID] = true
	}
	for _, id := range known {
		if onRecorder[id] {
			return nil
		}
	}
	return invalid("that address answers as a different recorder: none of the imported channels exist on it")
}

// NVRChannelView is a channel the recorder has now, with its live state and whether it already is a camera.
type NVRChannelView struct {
	provisionnvr.Channel
	Status   *provisionnvr.ChannelStatus
	Imported bool
}

// loadNVR loads a recorder the actor may manage and opens its client. The password is opened only for the call.
func (s *Service) loadNVR(ctx context.Context, actor authz.Actor, id uuid.UUID) (db.GetServerRow, *provisionnvr.Client, error) {
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if srv, err = q.GetServer(ctx, id); err != nil {
			return notFoundOr(err)
		}
		return c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID))
	})
	if err != nil {
		return srv, nil, err
	}
	if srv.Kind != KindProvisionNVR {
		return srv, nil, &UnsupportedKindError{Kind: srv.Kind}
	}
	pw, err := s.Sealer.Open(srv.PasswordSealed, srv.ID[:])
	if err != nil {
		return srv, nil, err
	}
	client, err := provisionnvr.NewClient(srv.BaseUrl, provisionnvr.NewCredentials(srv.Username, string(pw)), provisionnvr.Config{})
	if err != nil {
		return srv, nil, mapNVRErr(err)
	}
	return srv, client, nil
}

// ListProvisionNVRChannels reads the channels the recorder has now and marks the ones that already are cameras.
// Read-only; needs servers.manage.
func (s *Service) ListProvisionNVRChannels(ctx context.Context, actor authz.Actor, id uuid.UUID) ([]NVRChannelView, error) {
	srv, client, err := s.loadNVR(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Logout(ctx) }()
	channels, err := client.Channels(ctx)
	if err != nil {
		return nil, mapNVRErr(err)
	}
	statuses, _ := client.ChannelStatuses(ctx) // the state is a nicety; the list is not worth failing for it
	byID := map[string]provisionnvr.ChannelStatus{}
	for _, st := range statuses {
		byID[st.ID] = st
	}
	imported := map[string]bool{}
	err = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		cams, err := q.ListServerCameras(ctx, srv.ID)
		for _, c := range cams {
			imported[c.RemoteName] = true
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]NVRChannelView, 0, len(channels))
	for _, ch := range channels {
		v := NVRChannelView{Channel: ch, Imported: imported[ch.ID]}
		if st, ok := byID[ch.ID]; ok {
			v.Status = &st
		}
		out = append(out, v)
	}
	return out, nil
}

// ImportProvisionNVRChannels adds the chosen channels as cameras. A channel that is already a camera is left
// as it is; one deleted here earlier is skipped (restore it from the deleted cameras). Nothing is written to
// the recorder. Needs servers.manage.
func (s *Service) ImportProvisionNVRChannels(ctx context.Context, actor authz.Actor, id uuid.UUID, ids []string) (added, skipped int, err error) {
	if len(ids) == 0 {
		return 0, 0, invalid("choose at least one channel")
	}
	srv, client, err := s.loadNVR(ctx, actor, id)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = client.Logout(ctx) }()
	channels, err := client.Channels(ctx)
	if err != nil {
		return 0, 0, mapNVRErr(err)
	}
	byID := map[string]provisionnvr.Channel{}
	for _, ch := range channels {
		byID[ch.ID] = ch
	}
	chosen := make([]provisionnvr.Channel, 0, len(ids))
	seen := map[string]bool{}
	for _, cid := range ids {
		ch, ok := byID[cid]
		if !ok {
			return 0, 0, invalid("channel %q is not on the recorder", cid)
		}
		if !seen[cid] {
			seen[cid] = true
			chosen = append(chosen, ch)
		}
	}
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		for _, ch := range chosen {
			inserted, wasSkipped, err := upsertNVRChannel(ctx, q, srv.TenantID, srv.SiteID, srv.ID, ch)
			if err != nil {
				return err
			}
			switch {
			case wasSkipped:
				skipped++
			case inserted:
				added++
			}
		}
		return audit(ctx, q, actor, &srv.TenantID, ActionServerSynced, "server", srv.ID, map[string]any{
			"kind": KindProvisionNVR, "channels_added": added, "channels_skipped": skipped,
		})
	})
	if err != nil {
		return 0, 0, err
	}
	return added, skipped, nil
}
