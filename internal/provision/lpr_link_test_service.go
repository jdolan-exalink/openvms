package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

// LPR engines a camera can read plates with.
const (
	LPREngineProvision = "provision"
	LPREngineFrigate   = "frigate"
	LPREngineNone      = "none"
)

// Outcomes of one check of the LPR link test.
const (
	LPRCheckPass = "pass"
	LPRCheckFail = "fail"
	LPRCheckWarn = "warn"
	LPRCheckSkip = "skip"
)

// Limits and defaults of the test.
const (
	// LPRTestMaxWaitSeconds is the longest a test may wait for a keepalive.
	LPRTestMaxWaitSeconds = 30
	// lprOnlineWindow is how long after its last request a camera still counts as connected.
	lprOnlineWindow = 60 * time.Second
	// lprIngestionFresh is how stale the last pull of the agent queue may be: the worker pulls every 15 s.
	lprIngestionFresh = 2 * time.Minute
	// lprBacklogWarn is the number of queued events on the agent that suggests central is not draining it.
	lprBacklogWarn   = 100
	lprPollInterval  = 2 * time.Second
	lprKeepalivePath = "/SendKeepalive"
	lprEventPath     = "/v1/provision-lpr/event"
	lprAgentPort     = 7419
)

// LPRCheck is one verified link of the chain camera -> agent -> central.
type LPRCheck struct {
	ID      string
	Status  string
	Message string
	Detail  string
}

// LPRObserved carries the counters the test saw.
type LPRObserved struct {
	LastSeen       *time.Time
	KeepaliveCount *int64
	EventCount     *int64
	LastPlate      string
	LastEventTime  *time.Time
	Reads24h       *int
	LastReadAt     *time.Time
}

// LPRLinkResult is the answer of a link test.
type LPRLinkResult struct {
	Engine   string
	Passed   bool
	Checks   []LPRCheck
	Hints    []string
	Observed LPRObserved
}

func (r *LPRLinkResult) add(id, status, message, detail string) {
	r.Checks = append(r.Checks, LPRCheck{ID: id, Status: status, Message: message, Detail: detail})
}

func (r *LPRLinkResult) finish() {
	r.Passed = true
	for _, c := range r.Checks {
		if c.Status == LPRCheckFail {
			r.Passed = false
		}
	}
	if r.Hints == nil {
		r.Hints = []string{}
	}
}

// DetectLPREngine says which engine reads a camera's plates: the camera's own Provision analytics
// (tags provision or provision_isr, or a Provision ONVIF identity), Frigate's engine (the lpr tag or
// the mirrored lpr flag), or none.
func DetectLPREngine(tags []string, lprFlag bool, onvifCapabilities json.RawMessage) string {
	hasTag := func(want ...string) bool {
		for _, t := range tags {
			for _, w := range want {
				if strings.EqualFold(strings.TrimSpace(t), w) {
					return true
				}
			}
		}
		return false
	}
	if hasTag("provision", "provision_isr") || onvifIsProvision(onvifCapabilities) {
		return LPREngineProvision
	}
	if hasTag("lpr") || lprFlag {
		return LPREngineFrigate
	}
	return LPREngineNone
}

func onvifIsProvision(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var caps struct {
		Manufacturer string `json:"manufacturer"`
		Model        string `json:"model"`
	}
	if json.Unmarshal(raw, &caps) != nil {
		return false
	}
	return strings.Contains(strings.ToLower(caps.Manufacturer), "provision") || strings.Contains(strings.ToLower(caps.Model), "provision")
}

// LPRLinkAgent is what the link test reads from a server's agent and from central's ingestion state.
// *Service implements it.
type LPRLinkAgent interface {
	GetProvisionLPRStatus(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (map[string]any, error)
	GetProvisionLPRRegistry(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (LPRRegistry, error)
	GetProvisionLPRIngestion(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (LPRIngestion, error)
}

// LPRLinkTester checks the REST link of a Provision LPR camera to its server's agent. It only reads:
// it never registers the camera, writes to the agent or creates plate reads.
type LPRLinkTester struct {
	Agent LPRLinkAgent
	// Now and PollEvery are replaceable for tests; they default to the clock and 2 s.
	Now       func() time.Time
	PollEvery time.Duration
}

// LPRProvisionTarget describes the camera under test.
type LPRProvisionTarget struct {
	CameraID    uuid.UUID
	ServerID    uuid.UUID
	CameraIP    string
	RemoteName  string
	DisplayName string
	// TargetHost is the address the camera must call (the agent's host); empty when unknown.
	TargetHost string
	// WaitSeconds, when positive, polls the agent until a keepalive newer than the call start arrives.
	WaitSeconds int
}

// ValidateLPRTestWait rejects waits outside 0..LPRTestMaxWaitSeconds.
func ValidateLPRTestWait(seconds int) error {
	if seconds < 0 || seconds > LPRTestMaxWaitSeconds {
		return &ValidationError{Msg: fmt.Sprintf("wait_seconds must be between 0 and %d", LPRTestMaxWaitSeconds)}
	}
	return nil
}

func (t *LPRLinkTester) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *LPRLinkTester) pollEvery() time.Duration {
	if t.PollEvery > 0 {
		return t.PollEvery
	}
	return lprPollInterval
}

// FindLPRCameraStatus finds a camera's entry in the agent's status: by the ID it is registered
// under, by the address it connects from, or by the names older setups used. An empty address or name
// never matches an entry that has the same field empty.
func FindLPRCameraStatus(status map[string]any, camID uuid.UUID, cameraIP, remoteName, displayName string) map[string]any {
	list, _ := status["cameras"].([]any)
	for _, item := range list {
		c, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cIP, _ := c["camera_ip"].(string)
		cID, _ := c["camera_id"].(string)
		if (cameraIP != "" && cIP == cameraIP) || (cID != "" && (cID == remoteName || cID == displayName || cID == camID.String())) {
			return c
		}
	}
	return nil
}

func statusTime(v any) *time.Time {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || ts.IsZero() || ts.Year() <= 1 {
		return nil
	}
	return &ts
}

func statusInt(v any) *int64 {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	n := int64(f)
	return &n
}

// cameraSnapshot is what the agent's status says about one camera.
type cameraSnapshot struct {
	found          bool
	lastSeen       *time.Time
	keepaliveCount *int64
	eventCount     *int64
	lastPlate      string
	lastEventTime  *time.Time
}

func snapshotOf(entry map[string]any) cameraSnapshot {
	if entry == nil {
		return cameraSnapshot{}
	}
	plate, _ := entry["last_plate"].(string)
	return cameraSnapshot{
		found:          true,
		lastSeen:       statusTime(entry["last_seen"]),
		keepaliveCount: statusInt(entry["keepalive_count"]),
		eventCount:     statusInt(entry["event_count"]),
		lastPlate:      plate,
		lastEventTime:  statusTime(entry["last_event_time"]),
	}
}

func (s cameraSnapshot) observe(o *LPRObserved) {
	o.LastSeen, o.KeepaliveCount, o.EventCount = s.lastSeen, s.keepaliveCount, s.eventCount
	o.LastPlate, o.LastEventTime = s.lastPlate, s.lastEventTime
}

func (s cameraSnapshot) hasEvents() bool {
	return (s.eventCount != nil && *s.eventCount > 0) || s.lastEventTime != nil
}

// CheckProvision runs the Provision engine checks. A non-nil error means the test could not finish
// (the context ended); every other problem is reported as a check.
func (t *LPRLinkTester) CheckProvision(ctx context.Context, actor authz.Actor, target LPRProvisionTarget) (LPRLinkResult, error) {
	res := LPRLinkResult{Engine: LPREngineProvision}
	res.Hints = provisionHints(target.TargetHost)
	started := t.now()

	status, statusErr := t.Agent.GetProvisionLPRStatus(ctx, actor, target.ServerID)
	if err := ctx.Err(); err != nil {
		return res, err
	}
	switch {
	case errors.Is(statusErr, ErrAgentUnprovisioned):
		res.add("agent_configured", LPRCheckFail, "El servidor no tiene un agente registrado", "Instalá o registrá el agente del servidor para poder recibir las lecturas de la cámara.")
		res.add("agent_reachable", LPRCheckSkip, "Sin agente no se puede comprobar el alcance", "")
		res.add("receiver_listening", LPRCheckSkip, "Sin agente no hay receptor", "")
		res.add("registered", LPRCheckSkip, "Sin agente no hay registro de cámaras", "")
		res.add("enforcement", LPRCheckSkip, "Sin agente no hay registro de cámaras", "")
		res.add("keepalive_recent", LPRCheckSkip, "Sin agente la cámara no tiene a quién reportar", "")
		res.add("event_seen", LPRCheckSkip, "Sin agente no se reciben lecturas", "")
		t.checkIngestion(ctx, actor, target.ServerID, &res, false)
		res.finish()
		return res, nil
	case errors.Is(statusErr, ErrAgentProbeUnavailable), errors.Is(statusErr, ErrAgentProbeFailed):
		res.add("agent_configured", LPRCheckPass, "El servidor tiene un agente registrado", "")
		res.add("agent_reachable", LPRCheckFail, "OpenVMS no puede comunicarse con el agente", statusErr.Error())
		res.Hints = append(res.Hints, "Verificá que el agente esté en ejecución y que el servidor de OpenVMS lo alcance por red.")
		for _, id := range []string{"receiver_listening", "registered", "enforcement", "keepalive_recent", "event_seen"} {
			res.add(id, LPRCheckSkip, "No se pudo comprobar porque el agente no responde", "")
		}
		t.checkIngestion(ctx, actor, target.ServerID, &res, false)
		res.finish()
		return res, nil
	}
	res.add("agent_configured", LPRCheckPass, "El servidor tiene un agente registrado", "")
	res.add("agent_reachable", LPRCheckPass, "OpenVMS alcanza al agente", "")
	if statusErr != nil {
		res.add("receiver_listening", LPRCheckFail, "El receptor Provision LPR del agente no respondió", statusErr.Error())
		res.Hints = append(res.Hints, "Actualizá el agente de este servidor: una versión anterior no incluye el receptor Provision LPR.")
		for _, id := range []string{"registered", "enforcement", "keepalive_recent", "event_seen"} {
			res.add(id, LPRCheckSkip, "No se pudo comprobar porque el receptor no responde", "")
		}
		t.checkIngestion(ctx, actor, target.ServerID, &res, true)
		res.finish()
		return res, nil
	}
	res.add("receiver_listening", LPRCheckPass, "El receptor Provision LPR del agente responde", "")

	t.checkRegistry(ctx, actor, target, &res)
	if err := ctx.Err(); err != nil {
		return res, err
	}

	snap := snapshotOf(FindLPRCameraStatus(status, target.CameraID, target.CameraIP, target.RemoteName, target.DisplayName))
	var err error
	if target.WaitSeconds > 0 {
		snap, err = t.waitForKeepalive(ctx, actor, target, snap, started)
		if err != nil {
			return res, err
		}
	}
	snap.observe(&res.Observed)
	t.checkKeepalive(&res, snap, started, target.WaitSeconds > 0)
	t.checkEvents(&res, snap)
	t.checkIngestion(ctx, actor, target.ServerID, &res, true)
	if err := ctx.Err(); err != nil {
		return res, err
	}
	res.finish()
	return res, nil
}

func provisionHints(host string) []string {
	if host == "" {
		return []string{
			fmt.Sprintf("La cámara debe llamar al agente (puerto %d) en las rutas %s (keepalive) y %s (lecturas).", lprAgentPort, lprKeepalivePath, lprEventPath),
		}
	}
	return []string{
		fmt.Sprintf("La cámara debe llamar a http://%s:%d%s (keepalive) y http://%s:%d%s (lecturas).", host, lprAgentPort, lprKeepalivePath, host, lprAgentPort, lprEventPath),
	}
}

func (t *LPRLinkTester) checkRegistry(ctx context.Context, actor authz.Actor, target LPRProvisionTarget, res *LPRLinkResult) {
	reg, err := t.Agent.GetProvisionLPRRegistry(ctx, actor, target.ServerID)
	switch {
	case errors.Is(err, ErrAgentRegistryUnsupported):
		res.add("registered", LPRCheckFail, "El agente es demasiado antiguo para tener registro de cámaras", err.Error())
		res.add("enforcement", LPRCheckSkip, "El agente no tiene registro de cámaras", "")
		res.Hints = append(res.Hints, "Actualizá el agente de este servidor.")
		return
	case err != nil:
		res.add("registered", LPRCheckFail, "No se pudo leer el registro de cámaras del agente", err.Error())
		res.add("enforcement", LPRCheckSkip, "No se pudo leer el registro de cámaras del agente", "")
		return
	}
	registered := false
	for _, c := range reg.Cameras {
		if c.CameraID == target.CameraID.String() || (target.CameraIP != "" && c.IP == target.CameraIP) {
			registered = true
			break
		}
	}
	if registered {
		res.add("registered", LPRCheckPass, "La cámara está registrada en el agente", "")
	} else {
		res.add("registered", LPRCheckFail, "La cámara no está registrada en el agente", "Registrá la cámara con Configurar y cargá los valores en la cámara.")
		res.Hints = append(res.Hints, "Registrá la cámara con Configurar y cargá los valores en la cámara")
	}
	if reg.Enforce {
		res.add("enforcement", LPRCheckPass, "El agente solo acepta cámaras registradas", "")
	} else {
		res.add("enforcement", LPRCheckWarn, "El agente acepta peticiones de cualquier dirección", "El control de cámaras registradas está apagado: cualquier IP de la red podría enviar lecturas. Activalo cuando la cámara ya esté registrada.")
	}
}

// waitForKeepalive polls the agent status every PollEvery until the camera's last keepalive is newer
// than started, or WaitSeconds pass. It returns the latest snapshot it saw.
func (t *LPRLinkTester) waitForKeepalive(ctx context.Context, actor authz.Actor, target LPRProvisionTarget, snap cameraSnapshot, started time.Time) (cameraSnapshot, error) {
	deadline := time.NewTimer(time.Duration(target.WaitSeconds) * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(t.pollEvery())
	defer tick.Stop()
	for snap.lastSeen == nil || !snap.lastSeen.After(started) {
		select {
		case <-ctx.Done():
			return snap, ctx.Err()
		case <-deadline.C:
			return snap, nil
		case <-tick.C:
			status, err := t.Agent.GetProvisionLPRStatus(ctx, actor, target.ServerID)
			if ctx.Err() != nil {
				return snap, ctx.Err()
			}
			if err != nil {
				return snap, nil
			}
			snap = snapshotOf(FindLPRCameraStatus(status, target.CameraID, target.CameraIP, target.RemoteName, target.DisplayName))
		}
	}
	return snap, nil
}

func (t *LPRLinkTester) checkKeepalive(res *LPRLinkResult, snap cameraSnapshot, started time.Time, waited bool) {
	switch {
	case !snap.found || snap.lastSeen == nil:
		msg := "El agente nunca recibió un keepalive de la cámara"
		if waited {
			msg = "No llegó ningún keepalive de la cámara durante la espera"
		}
		res.add("keepalive_recent", LPRCheckFail, msg, "Revisá en la cámara la dirección y el puerto del servidor y que la cámara alcance al agente por red.")
	case t.now().Sub(*snap.lastSeen) > lprOnlineWindow && !snap.lastSeen.After(started):
		res.add("keepalive_recent", LPRCheckFail, "El último keepalive es antiguo: la cámara dejó de reportar",
			fmt.Sprintf("Último keepalive: %s (la ventana es de %d s).", snap.lastSeen.UTC().Format(time.RFC3339), int(lprOnlineWindow.Seconds())))
	default:
		res.add("keepalive_recent", LPRCheckPass, "La cámara está enviando keepalives al agente", fmt.Sprintf("Último keepalive: %s.", snap.lastSeen.UTC().Format(time.RFC3339)))
	}
}

func (t *LPRLinkTester) checkEvents(res *LPRLinkResult, snap cameraSnapshot) {
	connected := false
	for _, c := range res.Checks {
		if c.ID == "keepalive_recent" && c.Status == LPRCheckPass {
			connected = true
		}
	}
	switch {
	case snap.hasEvents():
		detail := ""
		if snap.lastPlate != "" {
			detail = "Última patente: " + snap.lastPlate + "."
		}
		res.add("event_seen", LPRCheckPass, "El agente recibió lecturas de patente de la cámara", detail)
	case connected:
		res.add("event_seen", LPRCheckWarn, "La cámara está conectada; falta una lectura de patente", "Pasá un vehículo frente a la cámara o esperá a que el análisis de patentes genere una lectura.")
	case !snap.found:
		res.add("event_seen", LPRCheckSkip, "Sin conexión de la cámara no hay lecturas que comprobar", "")
	default:
		res.add("event_seen", LPRCheckWarn, "Todavía no se vio ninguna lectura de patente", "")
	}
}

// checkIngestion judges how central drains the agent's queue. reachable says whether the agent
// answered, which decides how a missing pull is worded.
func (t *LPRLinkTester) checkIngestion(ctx context.Context, actor authz.Actor, serverID uuid.UUID, res *LPRLinkResult, reachable bool) {
	st, err := t.Agent.GetProvisionLPRIngestion(ctx, actor, serverID)
	switch {
	case err != nil:
		res.add("central_ingestion", LPRCheckFail, "No se pudo leer el estado de la ingesta central", err.Error())
	case !st.Known:
		if reachable {
			res.add("central_ingestion", LPRCheckWarn, "Central todavía no consultó la cola de lecturas de este agente", "El servicio worker de OpenVMS consulta cada pocos segundos; si persiste, verificá que el worker esté en ejecución.")
		} else {
			res.add("central_ingestion", LPRCheckSkip, "Central todavía no consultó este agente", "")
		}
	case st.LastError != "":
		res.add("central_ingestion", LPRCheckFail, "La última consulta de central a la cola del agente falló", st.LastError)
	case st.LastPullAt == nil || t.now().Sub(*st.LastPullAt) > lprIngestionFresh:
		res.add("central_ingestion", LPRCheckFail, "Central no consulta la cola del agente desde hace tiempo", "Verificá que el servicio worker de OpenVMS esté en ejecución.")
	case st.AgentLost > 0:
		res.add("central_ingestion", LPRCheckWarn, "El agente descartó lecturas antes de que central las recogiera", fmt.Sprintf("Lecturas perdidas: %d.", st.AgentLost))
	case st.AgentDepth > lprBacklogWarn:
		res.add("central_ingestion", LPRCheckWarn, "Hay lecturas acumuladas en el agente esperando a central", fmt.Sprintf("Lecturas en cola: %d.", st.AgentDepth))
	default:
		res.add("central_ingestion", LPRCheckPass, "Central recoge las lecturas del agente", fmt.Sprintf("Recogidas en total: %d.", st.IngestedTotal))
	}
}

// LPRReadsSummary is what central stored for a camera in a period.
type LPRReadsSummary struct {
	Count  int
	LastAt *time.Time
}

// LPRFrigateProbe are the lookups of the Frigate engine checks.
type LPRFrigateProbe struct {
	// LPREnabled reads the camera's effective Frigate configuration.
	LPREnabled func(ctx context.Context) (bool, error)
	// ServerLPR is the server's lpr capability.
	ServerLPR bool
	// Reads24h summarizes the camera's lpr_reads of the last 24 hours.
	Reads24h func(ctx context.Context) (LPRReadsSummary, error)
}

// CheckLPRFrigate runs the Frigate engine checks.
func CheckLPRFrigate(ctx context.Context, p LPRFrigateProbe) LPRLinkResult {
	res := LPRLinkResult{Engine: LPREngineFrigate}
	enabled, err := p.LPREnabled(ctx)
	switch {
	case err != nil:
		res.add("frigate_lpr_enabled", LPRCheckFail, "No se pudo leer la configuración de Frigate de la cámara", err.Error())
	case !enabled:
		res.add("frigate_lpr_enabled", LPRCheckFail, "El reconocimiento de patentes no está activado en la configuración de Frigate de esta cámara", "Activá lpr.enabled en la configuración de Frigate de la cámara.")
		res.Hints = append(res.Hints, "Activá el reconocimiento de patentes (lpr) en la configuración de Frigate de la cámara.")
	default:
		res.add("frigate_lpr_enabled", LPRCheckPass, "Frigate tiene activado el reconocimiento de patentes en esta cámara", "")
	}
	if p.ServerLPR {
		res.add("capability", LPRCheckPass, "El servidor Frigate declara soporte de LPR", "")
	} else {
		res.add("capability", LPRCheckFail, "El servidor Frigate no declara soporte de LPR", "Habilitá lpr en la configuración global de Frigate y reiniciá el servidor.")
		res.Hints = append(res.Hints, "Habilitá lpr en la configuración global de Frigate del servidor.")
	}
	reads, err := p.Reads24h(ctx)
	switch {
	case err != nil:
		res.add("reads_recent", LPRCheckWarn, "No se pudieron consultar las lecturas de la cámara", err.Error())
	case reads.Count == 0:
		res.add("reads_recent", LPRCheckWarn, "No hay lecturas de patente de esta cámara en las últimas 24 horas", "Pasá un vehículo frente a la cámara y volvé a probar.")
	default:
		res.add("reads_recent", LPRCheckPass, "La cámara tiene lecturas de patente en las últimas 24 horas", fmt.Sprintf("Lecturas: %d.", reads.Count))
	}
	if err == nil {
		n := reads.Count
		res.Observed.Reads24h = &n
		res.Observed.LastReadAt = reads.LastAt
	}
	res.finish()
	return res
}

// NotLPRResult is the answer for a camera that reads no plates.
func NotLPRResult() LPRLinkResult {
	res := LPRLinkResult{Engine: LPREngineNone}
	res.add("not_lpr", LPRCheckSkip, "La cámara no es LPR", "")
	res.finish()
	return res
}
