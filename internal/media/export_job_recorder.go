package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
)

// maxRecorderCaptures bounds the copies running against one recorder at a time: each holds an RTSP session for
// at least the length of its clip, and a recorder serves only a few at once.
const maxRecorderCaptures = 2

// offerLatest puts v in a one-slot channel without ever waiting, replacing a value nobody took yet.
func offerLatest(ch chan float32, v float32) {
	for {
		select {
		case ch <- v:
			return
		default:
		}
		select {
		case <-ch:
		default:
		}
	}
}

// recorderSlot waits for one of the recorder's capture slots and returns the function that frees it.
func (m *ExportJobManager) recorderSlot(ctx context.Context, server uuid.UUID) (func(), error) {
	m.mu.Lock()
	if m.recorderSlots == nil {
		m.recorderSlots = map[uuid.UUID]chan struct{}{}
	}
	slots, ok := m.recorderSlots[server]
	if !ok {
		slots = make(chan struct{}, maxRecorderCaptures)
		m.recorderSlots[server] = slots
	}
	m.mu.Unlock()
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// startRecorderExport moves a recorder camera's item straight to "transferring": there is no remote export to
// wait for, the worker copies the recorder's replay itself.
func (m *ExportJobManager) startRecorderExport(ctx context.Context, j activeJobRow, it *activeItemRow) {
	if m.NVR == nil {
		m.failItem(ctx, it.id, "exporting recorder cameras is not enabled on this deployment")
		return
	}
	_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE export_job_items SET status = 'transferring', progress = 0, updated_at = now() WHERE id = $1`, it.id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE export_jobs SET status = 'transferring', updated_at = now() WHERE id = $1 AND status = 'queued'`, j.id)
		return err
	})
	it.status = "transferring"
	m.ensureTransferring(ctx, j, *it)
}

// captureRecorderItem copies one recorder camera's video for the job's range into the job directory. The
// recorder plays at normal speed, so it takes about as long as the clip; progress follows the video written.
func (m *ExportJobManager) captureRecorderItem(ctx context.Context, j activeJobRow, it activeItemRow) error {
	if m.NVR == nil {
		return errors.New("exporting recorder cameras is not enabled on this deployment")
	}
	release, err := m.recorderSlot(ctx, it.serverID)
	if err != nil {
		return err
	}
	defer release()
	cam := Camera{ID: it.cameraID, RemoteName: it.remoteName, Server: it.serverRow}
	footage, err := m.NVR.RecordedWithin(ctx, cam, j.start, j.end)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "degraded") || errors.Is(err, ErrNoRecording) {
			return errors.New("Sin datos grabados en esta franja horaria (grabación por movimiento o sin actividad)")
		}
		return fmt.Errorf("the recorder could not be read: %w", err)
	}
	if footage <= 0 {
		return errors.New("Sin datos grabados en esta franja horaria (grabación por movimiento o sin actividad)")
	}
	rtsp, err := m.NVR.ReplayURL(ctx, cam, j.start, j.end, "main")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "degraded") || errors.Is(err, ErrNoRecording) {
			return errors.New("Sin datos grabados en esta franja horaria (grabación por movimiento o sin actividad)")
		}
		return err
	}
	ffmpeg := m.FFmpeg
	if ffmpeg == "" {
		if ffmpeg, err = exec.LookPath("ffmpeg"); err != nil {
			return errors.New("ffmpeg is not installed where the export runs")
		}
	}
	jobDir := filepath.Join(m.StorageDir, j.id.String())
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return err
	}
	_ = os.Chmod(jobDir, 0o775)
	partPath := filepath.Join(jobDir, it.id.String()+".mp4.part")
	finalPath := filepath.Join(jobDir, it.id.String()+".mp4")
	defer func() {
		if ctx.Err() != nil {
			_ = os.Remove(partPath)
		}
	}()

	total := j.end.Sub(j.start)
	// Progress is written by its own goroutine so a slow database never blocks ffmpeg's output pipe.
	progress := make(chan float32, 1)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for prog := range progress {
			_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `UPDATE export_job_items SET progress = $2, updated_at = now() WHERE id = $1`, it.id, prog); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `UPDATE export_jobs SET progress = (SELECT COALESCE(avg(progress), 0) FROM export_job_items WHERE job_id = $1), updated_at = now() WHERE id = $1`, j.id)
				return err
			})
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
			}
		}
	}()
	err = captureReplay(ctx, ffmpeg, rtsp, partPath, total, footage, func(done time.Duration) {
		prog := float32(done) / float32(total) * 100
		if prog > 99 {
			prog = 99
		}
		offerLatest(progress, prog)
	})
	close(progress)
	<-writerDone
	if err != nil {
		_ = os.Remove(partPath)
		return err
	}
	return m.completeItemFile(ctx, it, partPath, finalPath)
}

// sealFile hashes a finished capture and gives it its final name. An empty file means the recorder sent no video.
func sealFile(partPath, finalPath string) (hash string, size int64, err error) {
	f, err := os.Open(partPath) // #nosec G304 -- path composed from the private storage root and UUIDs
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	size, err = io.Copy(h, f)
	_ = f.Close()
	if err != nil {
		return "", 0, err
	}
	if size == 0 {
		_ = os.Remove(partPath)
		return "", 0, ErrNoRecording
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		return "", 0, err
	}
	_ = os.Chmod(finalPath, 0o664)
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// completeItemFile seals a finished file and records its hash and size: the item becomes ready.
func (m *ExportJobManager) completeItemFile(ctx context.Context, it activeItemRow, partPath, finalPath string) error {
	hash, size, err := sealFile(partPath, finalPath)
	if err != nil {
		return err
	}
	return m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
UPDATE export_job_items
SET status = 'ready', local_path = $2, sha256_hash = $3, total_bytes = $4, transferred_bytes = $4, progress = 100, updated_at = now()
WHERE id = $1`, it.id, finalPath, hash, size)
		return err
	})
}
