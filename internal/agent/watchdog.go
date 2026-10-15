package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// DefaultStorageSafetyBufferPercent is the default percentage of storage reserved to prevent data corruption.
	DefaultStorageSafetyBufferPercent = 15
)

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

// EnforceStorageSafety checks whether CCTV storage has breached the safety buffer (usage > 85%),
// and if enabled, prunes the oldest recording segments until free space is above the safety threshold.
func EnforceStorageSafety(cfg StorageWatchdogConfig, cctvTotal, cctvFree, dbTotal, dbFree uint64) (StorageWatchdogStatus, error) {
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
	if cctvTotal == 0 {
		return status, nil
	}

	minCCTVFree := uint64(float64(cctvTotal) * (float64(bufferPercent) / 100.0))
	if cctvFree < minCCTVFree {
		status.OverSafetyThreshold = true
		bytesNeeded := minCCTVFree - cctvFree

		if cfg.Enabled && cfg.RecordingsPath != "" {
			prunedCount, prunedBytes, err := pruneOldestRecordings(cfg.RecordingsPath, bytesNeeded)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return status, err
			}
			status.PrunedSegmentsCount = prunedCount
			status.PrunedBytes = prunedBytes
		}
	}

	return status, nil
}

func pruneOldestRecordings(rootPath string, bytesNeeded uint64) (uint64, uint64, error) {
	var candidates []fileCandidate

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
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
		candidates = append(candidates, fileCandidate{
			path:    path,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

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
