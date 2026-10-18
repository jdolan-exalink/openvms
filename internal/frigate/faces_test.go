package frigate

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
)

func startFaceMock(t *testing.T, version string, faceRecognition bool) (*httptest.Server, *frigatemock.Server) {
	t.Helper()
	cams, _ := frigatemock.ParseCameras("acceso_norte,plaza:centro")
	srv := &frigatemock.Server{Version: version, Cameras: cams, Store: frigatemock.NewStore(10), User: "vms", Password: "pw",
		RequireAuth: true, StartedAt: time.Now(), FaceRecognition: faceRecognition}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv
}

func TestFaceLibraryLifecycle(t *testing.T) {
	for _, version := range []string{"0.17.2-mock", "0.18.0-mock"} {
		t.Run(version, func(t *testing.T) {
			ts, mock := startFaceMock(t, version, true)
			a := connectMock(t, ts)
			ctx := context.Background()

			lib, err := a.ListFaces(ctx)
			if err != nil || len(lib.Faces) != 0 || len(lib.Train) != 0 {
				t.Fatalf("empty library = %+v, %v", lib, err)
			}
			if err := a.CreateFace(ctx, "ana_gomez"); err != nil {
				t.Fatal(err)
			}
			if err := a.RegisterFaceImage(ctx, "ana_gomez", "ana.png", []byte("png-bytes")); err != nil {
				t.Fatal(err)
			}
			if err := a.RegisterFaceImage(ctx, "ana_gomez", "ana2.jpg", []byte("jpg-bytes")); err != nil {
				t.Fatal(err)
			}
			lib, err = a.ListFaces(ctx)
			if err != nil {
				t.Fatal(err)
			}
			ids := lib.Faces["ana_gomez"]
			if len(ids) != 2 {
				t.Fatalf("want 2 images, got %v", lib.Faces)
			}
			if got := mock.FaceImageBytes("ana_gomez", ids[0]); string(got) != "png-bytes" && string(got) != "jpg-bytes" {
				t.Errorf("multipart file not received: %q", got)
			}

			if err := a.RenameFace(ctx, "ana_gomez", "ana_g"); err != nil {
				t.Fatal(err)
			}
			lib, _ = a.ListFaces(ctx)
			if _, old := lib.Faces["ana_gomez"]; old || len(lib.Faces["ana_g"]) != 2 {
				t.Fatalf("rename not applied: %v", lib.Faces)
			}

			if err := a.DeleteFaceImages(ctx, "ana_g", ids[:1]); err != nil {
				t.Fatal(err)
			}
			lib, _ = a.ListFaces(ctx)
			if len(lib.Faces["ana_g"]) != 1 {
				t.Fatalf("delete not applied: %v", lib.Faces)
			}

			// Train attempts surface separately and can be classified into a person.
			mock.AddFaceTrainAttempt("1700000000-abc-0.9.webp")
			lib, _ = a.ListFaces(ctx)
			if !reflect.DeepEqual(lib.Train, []string{"1700000000-abc-0.9.webp"}) {
				t.Fatalf("train attempts = %v", lib.Train)
			}
			if _, ok := lib.Faces["train"]; ok {
				t.Error("train must not be listed as a person")
			}
			if err := a.ClassifyTrainAttempt(ctx, "ana_g", "1700000000-abc-0.9.webp"); err != nil {
				t.Fatal(err)
			}
			lib, _ = a.ListFaces(ctx)
			if len(lib.Train) != 0 || len(lib.Faces["ana_g"]) != 2 {
				t.Fatalf("classify not applied: %+v", lib)
			}
			if err := a.ClassifyTrainAttempt(ctx, "ana_g", "missing.webp"); !errors.Is(err, ErrNotFound) {
				t.Errorf("classify missing = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestFaceLibraryDisabledFaceRecognition(t *testing.T) {
	ts, _ := startFaceMock(t, "0.17.2-mock", false)
	a := connectMock(t, ts)
	ctx := context.Background()
	if err := a.CreateFace(ctx, "ana"); !errors.Is(err, ErrFaceRecognitionDisabled) {
		t.Errorf("create = %v, want ErrFaceRecognitionDisabled", err)
	}
	if err := a.RegisterFaceImage(ctx, "ana", "a.png", []byte("x")); !errors.Is(err, ErrFaceRecognitionDisabled) {
		t.Errorf("register = %v", err)
	}
	if err := a.DeleteFaceImages(ctx, "ana", []string{"x.webp"}); !errors.Is(err, ErrFaceRecognitionDisabled) {
		t.Errorf("delete = %v", err)
	}
	if err := a.RenameFace(ctx, "ana", "bea"); !errors.Is(err, ErrFaceRecognitionDisabled) {
		t.Errorf("rename = %v", err)
	}
	if err := a.ClassifyTrainAttempt(ctx, "ana", "x.webp"); !errors.Is(err, ErrFaceRecognitionDisabled) {
		t.Errorf("classify = %v", err)
	}
}

func TestRenameFaceUnsupportedWhenRouteMissing(t *testing.T) {
	ts, mock := startFaceMock(t, "0.17.2-mock", true)
	mock.FaceRenameUnsupported = true
	a := connectMock(t, ts)
	if err := a.CreateFace(context.Background(), "ana"); err != nil {
		t.Fatal(err)
	}
	if err := a.RenameFace(context.Background(), "ana", "bea"); !errors.Is(err, ErrFaceRenameUnsupported) {
		t.Fatalf("rename = %v, want ErrFaceRenameUnsupported", err)
	}
}

func TestFaceNamesAreValidatedBeforeAnyRequest(t *testing.T) {
	ts, _ := startFaceMock(t, "0.18.0-mock", true)
	a := connectMock(t, ts)
	ctx := context.Background()
	for _, bad := range []string{"", "Ana", "a b", "../x", "a/b", strings.Repeat("a", 51)} {
		if err := a.CreateFace(ctx, bad); !errors.Is(err, ErrInvalidFaceName) {
			t.Errorf("CreateFace(%q) = %v, want ErrInvalidFaceName", bad, err)
		}
	}
	if err := a.RenameFace(ctx, "ana", "B!"); !errors.Is(err, ErrInvalidFaceName) {
		t.Errorf("rename to invalid = %v", err)
	}
	if err := a.DeleteFaceImages(ctx, "ana", nil); err == nil {
		t.Error("deleting zero ids must be rejected")
	}
}

func TestPerCameraFaceRecognitionCapability(t *testing.T) {
	ts, _ := startFaceMock(t, "0.17.2-mock", true)
	a := connectMock(t, ts)
	caps, err := a.Capabilities(context.Background())
	if err != nil || !caps.FaceRecognition {
		t.Fatalf("caps = %+v, %v", caps, err)
	}
	cams, err := a.ListCameras(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cams {
		if !c.FaceRecognition {
			t.Errorf("camera %s should inherit face recognition from the global toggle", c.Name)
		}
	}
	off, _ := startFaceMock(t, "0.17.2-mock", false)
	cams, _ = connectMock(t, off).ListCameras(context.Background())
	for _, c := range cams {
		if c.FaceRecognition {
			t.Errorf("camera %s must not report face recognition when disabled", c.Name)
		}
	}
}
