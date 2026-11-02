package agent

import (
	"container/heap"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// DefaultStorageSafetyBufferPercent is the default percentage of storage reserved to prevent data corruption.
	DefaultStorageSafetyBufferPercent = 15

	// storagePruneInterval is how often the opt-in pruner looks at the disk, and storagePruneTimeout
	// how long one pass may walk the recordings before it gives up.
	storagePruneInterval = 10 * time.Minute
	storagePruneTimeout  = 10 * time.Minute
)

// maxPruneCandidates bounds how many files one pass keeps in memory while it looks for the oldest.
// A recorder holds millions of segments; keeping them all made the agent grow by gigabytes. It is a
// variable only so a test can lower it.
var maxPruneCandidates = 50_000

// StorageWatchdogConfig holds parameters for edge agent storage protection.
type StorageWatchdogConfig struct {
	Enabled             bool
	SafetyBufferPercent int
	RecordingsPath      string
	DBPath              string
}

// StorageWatchdogStatus reports current status of storage protection.
type StorageWatchdogStatus struct {
	Enabled             bool    `json:"enabled"`
	SafetyBufferPercent int     `json:"safety_buffer_percent"`
	CCTVTotalBytes      uint64  `json:"cctv_total_bytes"`
	CCTVFreeBytes       uint64  `json:"cctv_free_bytes"`
	CCTVUsableBytes     uint64  `json:"cctv_usable_bytes"`
	CCTVUsedBytes       uint64  `json:"cctv_used_bytes"`
	DBTotalBytes        uint64  `json:"db_total_bytes"`
	DBFreeBytes         uint64  `json:"db_free_bytes"`
	OverSafetyThreshold bool    `json:"over_safety_threshold"`
	DBCritical          bool    `json:"db_critical"`
	PrunedSegmentsCount uint64  `json:"pruned_segments_count"`
	PrunedBytes         uint64  `json:"pruned_bytes"`
	EstimatedDays       float64 `json:"estimated_days"`
}

type fileCandidate struct {
	path    string
	size    int64
	modTime time.Time
}

// StorageSafetyStatus reports how full the storage is against the safety buffer. It only reads the
// numbers it is given: it never touches the recordings, so it is safe on the metrics path.
func StorageSafetyStatus(cfg StorageWatchdogConfig, cctvTotal, cctvFree, dbTotal, dbFree uint64) StorageWatchdogStatus {
	bufferPercent := cfg.SafetyBufferPercent
	if bufferPercent <= 0 || bufferPercent > 100 {
		bufferPercent = DefaultStorageSafetyBufferPercent
	}

	status := StorageWatchdogStatus{
		Enabled:             cfg.Enabled,
		SafetyBufferPercent: bufferPercent,
		CCTVTotalBytes:      cctvTotal,
		CCTVFreeBytes:       cctvFree,
		DBTotalBytes:        dbTotal,
		DBFreeBytes:         dbFree,
	}

	if cctvTotal > 0 {
		status.CCTVUsableBytes = uint64(float64(cctvTotal) * (1.0 - float64(bufferPercent)/100.0))
		if cctvTotal > cctvFree {
			status.CCTVUsedBytes = cctvTotal - cctvFree
		}
	}

	// Database mount check
	if dbTotal > 0 {
		minDBFree := uint64(float64(dbTotal) * 0.15)
		if dbFree < minDBFree {
			status.DBCritical = true
		}
	}

	// CCTV storage check
	if cctvTotal > 0 {
		minCCTVFree := uint64(float64(cctvTotal) * (float64(bufferPercent) / 100.0))
		if cctvFree < minCCTVFree {
			status.OverSafetyThreshold = true
		}
	}
	return status
}

// EnforceStorageSafety checks whether CCTV storage has breached the safety buffer (usage > 85%),
// and if enabled, prunes the oldest recording segments until free space is above the safety threshold.
// Pruning walks the whole recordings tree: never call it from a request. The agent runs it from the
// opt-in RunStoragePruner.
func EnforceStorageSafety(cfg StorageWatchdogConfig, cctvTotal, cctvFree, dbTotal, dbFree uint64) (StorageWatchdogStatus, error) {
	return EnforceStorageSafetyContext(context.Background(), cfg, cctvTotal, cctvFree, dbTotal, dbFree)
}

// EnforceStorageSafetyContext is EnforceStorageSafety with a context that stops the walk.
func EnforceStorageSafetyContext(ctx context.Context, cfg StorageWatchdogConfig, cctvTotal, cctvFree, dbTotal, dbFree uint64) (StorageWatchdogStatus, error) {
	status := StorageSafetyStatus(cfg, cctvTotal, cctvFree, dbTotal, dbFree)
	if !status.OverSafetyThreshold || !cfg.Enabled || cfg.RecordingsPath == "" {
		return status, nil
	}
	minCCTVFree := uint64(float64(cctvTotal) * (float64(status.SafetyBufferPercent) / 100.0))
	prunedCount, prunedBytes, err := pruneOldestRecordings(ctx, cfg.RecordingsPath, minCCTVFree-cctvFree)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return status, err
	}
	status.PrunedSegmentsCount = prunedCount
	status.PrunedBytes = prunedBytes
	return status, nil
}

// RunStoragePruner enforces the safety buffer in the background until ctx is done. It is opt-in
// (OPENVMS_AGENT_STORAGE_PRUNE): deleting footage behind the recorder's back is something an operator
// asks for, and it must never block the agent's API.
func RunStoragePruner(ctx context.Context, opts Paths, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	if opts.CCTV == "" {
		opts.CCTV = "/mnt/cctv"
	}
	ticker := time.NewTicker(storagePruneInterval)
	defer ticker.Stop()
	for {
		total, free := diskUsageFn(opts.CCTV)
		if status := StorageSafetyStatus(StorageWatchdogConfig{Enabled: true, RecordingsPath: opts.CCTV}, total, free, 0, 0); status.OverSafetyThreshold {
			passCtx, cancel := context.WithTimeout(ctx, storagePruneTimeout)
			done, err := EnforceStorageSafetyContext(passCtx, StorageWatchdogConfig{Enabled: true, RecordingsPath: opts.CCTV}, total, free, 0, 0)
			cancel()
			switch {
			case err != nil:
				log.Warn("storage pruning stopped", "error", err)
			default:
				log.Info("storage pruning pass finished", "segments", done.PrunedSegmentsCount, "bytes", done.PrunedBytes)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// oldestFirst keeps the n oldest files seen: the root is the newest of them, so a file older than the
// root replaces it and everything else is dropped without being stored.
type oldestFirst []fileCandidate

func (h oldestFirst) Len() int           { return len(h) }
func (h oldestFirst) Less(i, j int) bool { return h[i].modTime.After(h[j].modTime) }
func (h oldestFirst) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *oldestFirst) Push(x any)        { *h = append(*h, x.(fileCandidate)) }
func (h *oldestFirst) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func pruneOldestRecordings(ctx context.Context, rootPath string, bytesNeeded uint64) (uint64, uint64, error) {
	kept := &oldestFirst{}

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		cand := fileCandidate{path: path, size: info.Size(), modTime: info.ModTime()}
		switch {
		case kept.Len() < maxPruneCandidates:
			heap.Push(kept, cand)
		case cand.modTime.Before((*kept)[0].modTime):
			(*kept)[0] = cand
			heap.Fix(kept, 0)
		}
		return nil
	})
	if err != nil {
		// A walk that did not finish may have missed older files: delete nothing.
		return 0, 0, err
	}
	candidates := []fileCandidate(*kept)

	if len(candidates) == 0 {
		return 0, 0, nil
	}

	// Sort oldest first
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.Before(candidates[j].modTime)
	})

	var freedBytes uint64
	var prunedCount uint64

	for _, cand := range candidates {
		if err := os.Remove(cand.path); err == nil {
			prunedCount++
			if cand.size > 0 {
				freedBytes += uint64(cand.size)
			}
			if freedBytes >= bytesNeeded {
				break
			}
		}
	}

	return prunedCount, freedBytes, nil
}
