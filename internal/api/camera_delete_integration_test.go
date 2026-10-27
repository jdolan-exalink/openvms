//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func TestCameraDeleteAndRestoreOverHTTP(t *testing.T) {
	te := setupMapsTest(t, false)
	cam := te.Cameras["frigate-h01/plaza"]
	mock := te.Mocks["frigate-h01"].Server
	camPath := "/api/v1/cameras/" + cam.ID.String()
	listPath := "/api/v1/servers/" + cam.ServerID.String() + "/deleted-cameras"
	operator := te.Demo.Tokens["operador"]

	t.Run("a user without servers.manage is refused and nothing changes", func(t *testing.T) {
		if status, _, body := te.request(http.MethodDelete, camPath, operator, nil, nil); status != http.StatusForbidden {
			t.Fatalf("operator delete status = %d, want 403: %s", status, body)
		}
		if status, _, body := te.request(http.MethodGet, listPath, operator, nil, nil); status != http.StatusForbidden {
			t.Fatalf("operator list status = %d, want 403: %s", status, body)
		}
		if got := len(mock.SaveCalls()); got != 0 {
			t.Fatalf("a refused delete saved Frigate's config %d times", got)
		}
	})

	t.Run("an unknown camera is 404", func(t *testing.T) {
		if status, _, body := te.request(http.MethodDelete, "/api/v1/cameras/00000000-0000-0000-0000-000000000001", te.AdminToken, nil, nil); status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", status, body)
		}
	})

	t.Run("Frigate unreachable is 502 and the camera stays", func(t *testing.T) {
		mock.Offline.Store(true)
		defer mock.Offline.Store(false)
		if status, _, body := te.request(http.MethodDelete, camPath, te.AdminToken, nil, nil); status != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502: %s", status, body)
		}
		if status, _, body := te.request(http.MethodGet, camPath, te.AdminToken, nil, nil); status != http.StatusOK {
			t.Fatalf("the camera must still be live after a failed delete, got %d: %s", status, body)
		}
	})

	t.Run("delete is 204, the camera is gone and listed as deleted", func(t *testing.T) {
		if status, _, body := te.request(http.MethodDelete, camPath, te.AdminToken, nil, nil); status != http.StatusNoContent {
			t.Fatalf("status = %d, want 204: %s", status, body)
		}
		if status, _, _ := te.request(http.MethodGet, camPath, te.AdminToken, nil, nil); status != http.StatusNotFound {
			t.Fatalf("GET of a deleted camera = %d, want 404", status)
		}
		status, _, body := te.request(http.MethodGet, listPath, te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("list status = %d: %s", status, body)
		}
		var list struct{ Items []gen.DeletedCamera }
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != 1 || list.Items[0].Id != cam.ID || !list.Items[0].HasFrigateConfig ||
			list.Items[0].RetainDays == nil || *list.Items[0].RetainDays != 7 {
			t.Fatalf("deleted cameras = %+v, want the plaza with 7 recording days and its Frigate block kept", list.Items)
		}
	})

	t.Run("restore is refused to an operator and 200 to an admin with the same id", func(t *testing.T) {
		if status, _, body := te.request(http.MethodPost, camPath+"/restore", operator, nil, nil); status != http.StatusForbidden {
			t.Fatalf("operator restore status = %d, want 403: %s", status, body)
		}
		status, _, body := te.request(http.MethodPost, camPath+"/restore", te.AdminToken, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("restore status = %d, want 200: %s", status, body)
		}
		var restored gen.Camera
		if err := json.Unmarshal(body, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.Id != cam.ID {
			t.Fatalf("restored id = %s, want %s", restored.Id, cam.ID)
		}
		if status, _, body := te.request(http.MethodPost, camPath+"/restore", te.AdminToken, nil, nil); status != http.StatusNotFound {
			t.Fatalf("restoring a camera that is not deleted = %d, want 404: %s", status, body)
		}
	})
}
