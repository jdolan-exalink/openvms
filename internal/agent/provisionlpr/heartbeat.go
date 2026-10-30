package provisionlpr

import (
	"sync"
	"time"
)

// HeartbeatManager tracks camera connection state and heartbeat timestamps.
type HeartbeatManager struct {
	mu      sync.RWMutex
	timeout time.Duration
	cameras map[string]*CameraStatus
}

// NewHeartbeatManager creates a manager with the specified timeout.
func NewHeartbeatManager(timeout time.Duration) *HeartbeatManager {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &HeartbeatManager{
		timeout: timeout,
		cameras: make(map[string]*CameraStatus),
	}
}

// RecordKeepalive updates last seen timestamp and marks camera online.
func (m *HeartbeatManager) RecordKeepalive(cameraID, cameraIP string, now time.Time) {
	key := cameraKey(cameraID, cameraIP)
	m.mu.Lock()
	defer m.mu.Unlock()

	stat, exists := m.cameras[key]
	if !exists {
		stat = &CameraStatus{
			CameraID: cameraID,
			CameraIP: cameraIP,
		}
		m.cameras[key] = stat
	}
	stat.Online = true
	stat.LastSeen = now
	stat.KeepaliveCount++
}

// RecordFullEvent records a full LPR reading with vehicle attributes and image data for the camera.
func (m *HeartbeatManager) RecordFullEvent(event LPREvent, now time.Time) {
	key := cameraKey(event.CameraID, event.CameraIP)
	m.mu.Lock()
	defer m.mu.Unlock()

	stat, exists := m.cameras[key]
	if !exists {
		stat = &CameraStatus{
			CameraID: event.CameraID,
			CameraIP: event.CameraIP,
		}
		m.cameras[key] = stat
	}
	stat.Online = true
	stat.LastSeen = now
	stat.EventCount++
	if event.Plate != "" {
		stat.LastPlate = event.Plate
	}
	t := event.EventTime
	stat.LastEventTime = &t

	stat.RecentEvents = append([]LPREvent{event}, stat.RecentEvents...)
	if len(stat.RecentEvents) > 20 {
		stat.RecentEvents = stat.RecentEvents[:20]
	}
}

// RecordEvent records an LPR reading for the camera, updating last seen and event count.
func (m *HeartbeatManager) RecordEvent(cameraID, cameraIP, plate string, eventTime, now time.Time) {
	m.RecordFullEvent(LPREvent{
		CameraID:  cameraID,
		CameraIP:  cameraIP,
		Plate:     plate,
		EventTime: eventTime,
	}, now)
}

// Status returns the current status of a camera by ID or IP.
func (m *HeartbeatManager) Status(cameraID, cameraIP string, now time.Time) (CameraStatus, bool) {
	key := cameraKey(cameraID, cameraIP)
	m.mu.RLock()
	defer m.mu.RUnlock()

	stat, exists := m.cameras[key]
	if !exists {
		return CameraStatus{}, false
	}
	out := *stat
	if now.Sub(out.LastSeen) > m.timeout {
		out.Online = false
	}
	return out, true
}

// AllStatuses returns a copy of all camera statuses, updating online/offline state against now.
func (m *HeartbeatManager) AllStatuses(now time.Time) []CameraStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]CameraStatus, 0, len(m.cameras))
	for _, stat := range m.cameras {
		s := *stat
		if now.Sub(s.LastSeen) > m.timeout {
			s.Online = false
		}
		out = append(out, s)
	}
	return out
}

func cameraKey(cameraID, cameraIP string) string {
	if cameraID != "" {
		return cameraID
	}
	return cameraIP
}
