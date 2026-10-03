package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/hoststat"
)

type workerCapacity struct {
	Ready          bool    `json:"ready"`
	Measuring      bool    `json:"measuring"`
	Model          string  `json:"model"`
	LatencyMs      float64 `json:"latency_ms"`
	CropsPerSecond float64 `json:"crops_per_second"`
	CropsPerMinute float64 `json:"crops_per_minute"`
	Threads        int     `json:"threads"`
}

func (h *Handlers) GetSystemCapacity(ctx context.Context, _ gen.GetSystemCapacityRequestObject) (gen.GetSystemCapacityResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	snap := hoststat.Read(hoststat.Roots{
		Proc: os.Getenv("HOST_PROC"),
		DRM:  os.Getenv("HOST_DRM"),
		Disk: os.Getenv("HOST_DISK"),
	})
	body := gen.SystemCapacity{}
	body.Cpu.Model = snap.CPUModel
	body.Cpu.Online = snap.CPUOnline
	body.Cpu.Percent = float32(snap.CPUPercent)
	body.Memory.TotalBytes = int64(snap.MemTotal)
	body.Memory.AvailableBytes = int64(snap.MemAvailable)
	body.Disk.Path = snap.DiskPath
	body.Disk.TotalBytes = int64(snap.DiskTotal)
	body.Disk.FreeBytes = int64(snap.DiskFree)
	if len(snap.GPUs) > 0 {
		body.Gpu.Present = true
		body.Gpu.Vendor = snap.GPUs[0].Vendor
		body.Gpu.Name = snap.GPUs[0].Name
	}
	body.Openvino.Installed = snap.OpenVINO
	body.Openvino.Active = false
	body.Openvino.Runtime = "ONNX Runtime CPU"
	if measured, ok := h.workerCapacity(ctx); ok {
		body.Classifier.Installed = measured.Ready
		body.Classifier.Measuring = measured.Measuring
		if measured.Model != "" {
			body.Classifier.Model = &measured.Model
		}
		if measured.LatencyMs > 0 {
			ms := float32(measured.LatencyMs)
			perSec := float32(measured.CropsPerSecond)
			perMin := float32(measured.CropsPerMinute)
			body.Classifier.LatencyMs = &ms
			body.Classifier.CropsPerSecond = &perSec
			body.Classifier.CropsPerMinute = &perMin
		}
		if measured.Threads > 0 {
			body.Classifier.Threads = &measured.Threads
		}
	}
	if h.Inv != nil {
		_, cameras, err := h.Inv.ClassifyPolicy(ctx, a)
		if err == nil {
			body.Classifier.CamerasTotal = len(cameras)
			for _, cam := range cameras {
				if cam.Effective {
					body.Classifier.CamerasOn++
				}
			}
		}
	}
	return gen.GetSystemCapacity200JSONResponse(body), nil
}

func (h *Handlers) workerCapacity(ctx context.Context) (workerCapacity, bool) {
	if h.WorkerURL == "" {
		return workerCapacity{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.WorkerURL+"/capacity", nil)
	if err != nil {
		return workerCapacity{}, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return workerCapacity{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return workerCapacity{}, false
	}
	var out workerCapacity
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return workerCapacity{}, false
	}
	return out, true
}

func (h *Handlers) GetClassifyPolicy(ctx context.Context, _ gen.GetClassifyPolicyRequestObject) (gen.GetClassifyPolicyResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	servers, cameras, err := h.Inv.ClassifyPolicy(ctx, a)
	if err != nil {
		return nil, err
	}
	out := gen.ClassifyPolicy{}
	for _, srv := range servers {
		out.Servers = append(out.Servers, struct {
			Enabled bool               `json:"enabled"`
			Id      openapi_types.UUID `json:"id"`
		}{Enabled: srv.Enabled, Id: srv.ID})
	}
	for _, cam := range cameras {
		out.Cameras = append(out.Cameras, struct {
			Effective bool               `json:"effective"`
			Enabled   bool               `json:"enabled"`
			Id        openapi_types.UUID `json:"id"`
			ServerId  openapi_types.UUID `json:"server_id"`
		}{Effective: cam.Effective, Enabled: cam.Enabled, Id: cam.ID, ServerId: cam.ServerID})
	}
	if out.Servers == nil {
		out.Servers = []struct {
			Enabled bool               `json:"enabled"`
			Id      openapi_types.UUID `json:"id"`
		}{}
	}
	if out.Cameras == nil {
		out.Cameras = []struct {
			Effective bool               `json:"effective"`
			Enabled   bool               `json:"enabled"`
			Id        openapi_types.UUID `json:"id"`
			ServerId  openapi_types.UUID `json:"server_id"`
		}{}
	}
	return gen.GetClassifyPolicy200JSONResponse(out), nil
}

func (h *Handlers) SetServerBodyClassify(ctx context.Context, r gen.SetServerBodyClassifyRequestObject) (gen.SetServerBodyClassifyResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "enabled is required"}
	}
	if err := h.Inv.SetServerBodyClassify(ctx, a, r.ServerId, r.Body.Enabled); err != nil {
		return nil, err
	}
	return gen.SetServerBodyClassify200JSONResponse{Enabled: r.Body.Enabled}, nil
}

func (h *Handlers) SetCameraBodyClassify(ctx context.Context, r gen.SetCameraBodyClassifyRequestObject) (gen.SetCameraBodyClassifyResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "enabled is required"}
	}
	if err := h.Inv.SetCameraBodyClassify(ctx, a, r.CameraId, r.Body.Enabled); err != nil {
		return nil, err
	}
	return gen.SetCameraBodyClassify200JSONResponse{Enabled: r.Body.Enabled}, nil
}
