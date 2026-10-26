package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jdolan-exalink/openvms/internal/agent/backup"
	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

// backupBodyLimit caps a PUT body: the configuration may be backup.MaxConfigBytes and JSON escaping
// can double it, plus the other fields. The configuration itself is checked again by the store.
const backupBodyLimit = 2*backup.MaxConfigBytes + 64<<10

type backupPutRequest struct {
	Config         string `json:"config"`
	FrigateVersion string `json:"frigate_version"`
	RegisteredBy   string `json:"registered_by"`
}

type backupMeta struct {
	SavedAt        string `json:"saved_at"`
	FrigateVersion string `json:"frigate_version"`
	RegisteredBy   string `json:"registered_by"`
	SHA256         string `json:"sha256"`
	Size           int    `json:"size"`
}

type backupDocument struct {
	Config string `json:"config"`
	backupMeta
}

type backupConflict struct {
	Code string `json:"code"`
	backupMeta
}

func metaView(m backup.Meta) backupMeta {
	saved := ""
	if !m.SavedAt.IsZero() {
		saved = m.SavedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return backupMeta{SavedAt: saved, FrigateVersion: m.FrigateVersion, RegisteredBy: m.RegisteredBy, SHA256: m.SHA256, Size: m.Size}
}

func writeBackupJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeBackupError(w http.ResponseWriter, status int, code, message string) {
	writeBackupJSON(w, status, map[string]string{"code": code, "message": message})
}

// mountBackup registers the routes behind the original Frigate configuration backup. The file holds
// secrets (camera passwords), so every route needs the control scope. The configuration is never
// logged and never appears in an error answer.
func mountBackup(mux *http.ServeMux, auth *authenticator, store *backup.Store) {
	control := func(h http.HandlerFunc) http.Handler { return auth.scoped(tokens.ScopeControl, h) }

	mux.Handle("GET /v1/backup/original", control(func(w http.ResponseWriter, _ *http.Request) {
		config, meta, err := store.Load()
		switch {
		case errors.Is(err, backup.ErrNotFound):
			writeBackupError(w, http.StatusNotFound, "no_backup", "no original configuration backup is stored")
		case err != nil:
			slog.Error("original configuration backup unreadable", "error", err)
			writeBackupError(w, http.StatusInternalServerError, "backup_unreadable", "the backup cannot be read")
		default:
			writeBackupJSON(w, http.StatusOK, backupDocument{Config: config, backupMeta: metaView(meta)})
		}
	}))

	mux.Handle("PUT /v1/backup/original", control(func(w http.ResponseWriter, r *http.Request) {
		var req backupPutRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, backupBodyLimit))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeBackupError(w, http.StatusBadRequest, "invalid", "the request body must be JSON {config, frigate_version, registered_by} within the size limit")
			return
		}
		meta, err := store.Save(req.Config, req.FrigateVersion, req.RegisteredBy)
		switch {
		case err == nil:
			slog.Info("original Frigate configuration stored", "sha256", meta.SHA256, "size", meta.Size)
			writeBackupJSON(w, http.StatusCreated, metaView(meta))
		case errors.Is(err, backup.ErrExists):
			writeBackupJSON(w, http.StatusConflict, backupConflict{Code: "backup_exists", backupMeta: metaView(meta)})
		case errors.Is(err, backup.ErrInvalid):
			writeBackupError(w, http.StatusBadRequest, "invalid", err.Error())
		default:
			slog.Error("original configuration backup not stored", "error", err)
			writeBackupError(w, http.StatusInternalServerError, "backup_failed", "the backup could not be stored")
		}
	}))

	mux.Handle("DELETE /v1/backup/original", control(func(w http.ResponseWriter, _ *http.Request) {
		err := store.Delete()
		switch {
		case err == nil:
			slog.Info("original Frigate configuration backup deleted")
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, backup.ErrNotFound):
			writeBackupError(w, http.StatusNotFound, "no_backup", "no original configuration backup is stored")
		default:
			slog.Error("original configuration backup not deleted", "error", err)
			writeBackupError(w, http.StatusInternalServerError, "backup_failed", "the backup could not be deleted")
		}
	}))
}
