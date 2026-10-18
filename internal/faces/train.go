package faces

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	// trainTimeout bounds each remote call made on behalf of a request.
	trainTimeout = 12 * time.Second
	// maxTrainPerServer and maxTrainTotal bound the attempts one list returns.
	maxTrainPerServer = 100
	maxTrainTotal     = 300
)

// TrainAttempt is one unclassified recognition attempt Frigate saved (save_attempts). The file
// name is the handle; label, score and time are parsed from it on a best-effort basis (the name
// format `<event id>-<unix time>-<label>-<score>.webp` was not verified against a live Frigate)
// and stay nil when it does not match.
type TrainAttempt struct {
	ServerID   uuid.UUID
	ServerName string
	File       string
	SeenAt     *time.Time
	Label      string
	Score      *float64
}

// TrainServer reports how one face-enabled server answered. Error is a short, sanitized reason.
type TrainServer struct {
	ServerID   uuid.UUID
	ServerName string
	Error      string
}

type TrainList struct {
	Attempts []TrainAttempt
	Servers  []TrainServer
}

func parseTrainFile(file string) (seen *time.Time, label string, score *float64) {
	parts := strings.Split(strings.TrimSuffix(file, "."+extOf(file)), "-")
	if len(parts) < 4 {
		return nil, "", nil
	}
	sc, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return nil, "", nil
	}
	// Frigate 0.17 writes whole seconds; 0.18 adds a fraction (1792014467.366228).
	ts, err := strconv.ParseFloat(parts[len(parts)-3], 64)
	if err != nil || ts < 1 {
		return nil, "", nil
	}
	t := time.Unix(int64(ts), 0).UTC()
	return &t, parts[len(parts)-2], &sc
}

func extOf(file string) string {
	if i := strings.LastIndexByte(file, '.'); i >= 0 {
		return file[i+1:]
	}
	return ""
}

func (s *Service) adapter(ctx context.Context, srv db.FrigateServer) (frigate.Adapter, error) {
	if s.Adapters == nil {
		return nil, errors.New("frigate access is not configured")
	}
	return s.Adapters.Get(ctx, srv)
}

// remoteReason maps a remote failure to a short reason that never carries addresses or bodies.
func remoteReason(err error) string {
	switch {
	case errors.Is(err, frigate.ErrFaceRecognitionDisabled):
		return "face recognition is disabled on this server"
	case errors.Is(err, context.DeadlineExceeded):
		return "the server did not answer in time"
	default:
		return "the server could not be reached"
	}
}

// ListTrainAttempts lists the recent unclassified attempts of every face-enabled server of the
// tenant (faces.view). A server that fails is reported in Servers and does not fail the list.
func (s *Service) ListTrainAttempts(ctx context.Context, actor authz.Actor, tenantID uuid.UUID) (TrainList, error) {
	var servers []db.FrigateServer
	if err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		var err error
		servers, err = q.ListFaceCapableServers(ctx, tenantID)
		return err
	}); err != nil {
		return TrainList{}, err
	}
	type result struct {
		files []string
		err   error
	}
	results := make([]result, len(servers))
	var wg sync.WaitGroup
	for i, srv := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, trainTimeout)
			defer cancel()
			a, err := s.adapter(cctx, srv)
			if err == nil {
				var lib frigate.FaceLibrary
				if lib, err = a.ListFaces(cctx); err == nil {
					results[i].files = lib.Train
				}
			}
			results[i].err = err
		}()
	}
	wg.Wait()

	out := TrainList{Attempts: []TrainAttempt{}, Servers: make([]TrainServer, 0, len(servers))}
	for i, srv := range servers {
		ts := TrainServer{ServerID: srv.ID, ServerName: srv.Name}
		if results[i].err != nil {
			ts.Error = remoteReason(results[i].err)
			if s.Log != nil {
				s.Log.WarnContext(ctx, "face train list failed", "server_id", srv.ID, "error", results[i].err)
			}
		}
		out.Servers = append(out.Servers, ts)
		perServer := make([]TrainAttempt, 0, len(results[i].files))
		for _, f := range results[i].files {
			if !frigate.ValidFaceFile(f) {
				continue
			}
			seen, label, score := parseTrainFile(f)
			perServer = append(perServer, TrainAttempt{ServerID: srv.ID, ServerName: srv.Name, File: f, SeenAt: seen, Label: label, Score: score})
		}
		sortAttempts(perServer)
		out.Attempts = append(out.Attempts, perServer[:min(len(perServer), maxTrainPerServer)]...)
	}
	sortAttempts(out.Attempts)
	out.Attempts = out.Attempts[:min(len(out.Attempts), maxTrainTotal)]

	err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		return s.audit(ctx, q, actor, tenantID, ActionTrainRead, "face_train", uuid.Nil,
			map[string]any{"scope": "list", "servers": len(servers), "attempts": len(out.Attempts)})
	})
	return out, err
}

// sortAttempts orders newest first; attempts without a parsed time sort last by file name.
func sortAttempts(a []TrainAttempt) {
	sort.SliceStable(a, func(i, j int) bool {
		x, y := a[i].SeenAt, a[j].SeenAt
		switch {
		case x != nil && y != nil && !x.Equal(*y):
			return x.After(*y)
		case (x == nil) != (y == nil):
			return x != nil
		}
		return a[i].File > a[j].File
	})
}

// trainServer authorizes (perm) and resolves a server of the tenant; any other server is not found.
func (s *Service) trainServer(ctx context.Context, actor authz.Actor, tenantID, serverID uuid.UUID, perm authz.Permission) (db.FrigateServer, error) {
	var srv db.FrigateServer
	err := s.tx(ctx, actor, tenantID, perm, func(q *db.Queries) error {
		var err error
		if srv, err = q.GetFaceSyncServer(ctx, serverID); err != nil {
			return store.Classify(err)
		}
		if srv.TenantID != tenantID {
			return store.ErrNotFound
		}
		return nil
	})
	return srv, err
}

// discardNewPerson deletes a person created moments ago whose first image could not be added.
// It runs on a detached, bounded context because the failure is often the request context.
func (s *Service) discardNewPerson(ctx context.Context, actor authz.Actor, tenantID, personID uuid.UUID) {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err := s.DeletePerson(dctx, actor, tenantID, personID); err != nil && s.Log != nil {
		s.Log.ErrorContext(dctx, "faces: could not discard the person created for a failed assign", "person_id", personID, "error", err)
	}
}

// RemoteError is a failed call to a Frigate server; Reason is sanitized (never an address or
// a response body) and safe to show to the caller.
type RemoteError struct{ Reason string }

func (e *RemoteError) Error() string { return "frigate: " + e.Reason }

// unreachable turns a remote failure into a RemoteError; a missing attempt stays not-found.
func unreachable(err error) error {
	var ve *ValidationError
	if errors.Is(err, store.ErrNotFound) || errors.As(err, &ve) {
		return err
	}
	return &RemoteError{Reason: remoteReason(err)}
}

func fetchAttempt(ctx context.Context, a frigate.Adapter, file string) ([]byte, string, error) {
	cctx, cancel := context.WithTimeout(ctx, trainTimeout)
	defer cancel()
	data, ct, err := a.FaceTrainImage(cctx, file)
	if errors.Is(err, frigate.ErrNotFound) {
		return nil, "", store.ErrNotFound
	}
	if errors.Is(err, frigate.ErrFaceImageTooLarge) {
		return nil, "", invalid("the attempt picture is too large")
	}
	return data, ct, err
}

// TrainAttemptImage proxies the picture of one attempt (faces.view) and audits the read.
func (s *Service) TrainAttemptImage(ctx context.Context, actor authz.Actor, tenantID, serverID uuid.UUID, file string) ([]byte, string, error) {
	if !frigate.ValidFaceFile(file) {
		return nil, "", invalid("invalid attempt file name")
	}
	srv, err := s.trainServer(ctx, actor, tenantID, serverID, authz.FacesView)
	if err != nil {
		return nil, "", err
	}
	a, err := s.adapter(ctx, srv)
	if err != nil {
		return nil, "", unreachable(err)
	}
	data, ct, err := fetchAttempt(ctx, a, file)
	if err != nil {
		return nil, "", unreachable(err)
	}
	err = s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		return s.audit(ctx, q, actor, tenantID, ActionTrainRead, "face_train", serverID, map[string]any{"scope": "image", "server_id": serverID})
	})
	return data, ct, err
}

// AssignInput says which attempt becomes an enrollment image and for whom: exactly one of
// PersonID (an existing person) and NewPersonName (a person created for it).
type AssignInput struct {
	ServerID      uuid.UUID
	File          string
	PersonID      *uuid.UUID
	NewPersonName string
}

type AssignResult struct {
	Person Person
	Image  Image
	// AttemptRemoved reports whether the attempt was also deleted from Frigate (best effort).
	AttemptRemoved bool
}

// AssignTrainAttempt turns an attempt into an enrollment image (faces.manage). The picture is
// fetched from Frigate and added CENTRALLY, so the registry sync distributes it like any other
// image; classifying it directly in Frigate would be undone as drift. The Frigate attempt is
// then deleted best effort: the central copy already exists, so a failure there is harmless.
func (s *Service) AssignTrainAttempt(ctx context.Context, actor authz.Actor, tenantID uuid.UUID, in AssignInput) (AssignResult, error) {
	var res AssignResult
	if !frigate.ValidFaceFile(in.File) {
		return res, invalid("invalid attempt file name")
	}
	if (in.PersonID == nil) == (strings.TrimSpace(in.NewPersonName) == "") {
		return res, invalid("choose exactly one of an existing person or a new person name")
	}
	srv, err := s.trainServer(ctx, actor, tenantID, in.ServerID, authz.FacesManage)
	if err != nil {
		return res, err
	}
	a, err := s.adapter(ctx, srv)
	if err != nil {
		return res, unreachable(err)
	}
	data, ct, err := fetchAttempt(ctx, a, in.File)
	if err != nil {
		return res, unreachable(err)
	}
	// Frigate serves webp; parameters ("; charset") and unknown types fall back to sniffing.
	ct = strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
	if _, vErr := validateImage(data, ct); vErr != nil {
		if _, vErr2 := validateImage(data, http.DetectContentType(data)); vErr2 != nil {
			return res, vErr
		}
		ct = http.DetectContentType(data)
	}

	var personID uuid.UUID
	created := false
	if in.PersonID != nil {
		personID = *in.PersonID
	} else {
		p, err := s.CreatePerson(ctx, actor, tenantID, PersonInput{DisplayName: in.NewPersonName})
		if err != nil {
			return res, err
		}
		personID, created = p.ID, true
	}
	img, err := s.AddImage(ctx, actor, tenantID, personID, data, ct)
	if err != nil {
		if created {
			// The person exists only for this image: do not leave an empty orphan behind.
			s.discardNewPerson(ctx, actor, tenantID, personID)
		}
		return res, err
	}
	res.Image = img
	if err := s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		row, err := q.GetFacePerson(ctx, db.GetFacePersonParams{TenantID: tenantID, ID: personID})
		if err != nil {
			return store.Classify(err)
		}
		n, err := q.CountFacePersonImages(ctx, db.CountFacePersonImagesParams{TenantID: tenantID, PersonID: personID})
		if err != nil {
			return err
		}
		res.Person = toPerson(row, int(n))
		return s.audit(ctx, q, actor, tenantID, ActionTrainAssigned, "face_person", personID,
			map[string]any{"person_id": personID, "image_id": img.ID, "server_id": in.ServerID})
	}); err != nil {
		return res, err
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), trainTimeout)
	defer cancel()
	if err := a.DeleteFaceImages(dctx, "train", []string{in.File}); err != nil {
		if s.Log != nil {
			s.Log.WarnContext(ctx, "face train attempt not removed from frigate", "server_id", in.ServerID, "error", err)
		}
	} else {
		res.AttemptRemoved = true
	}
	return res, nil
}
