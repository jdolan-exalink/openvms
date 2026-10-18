package frigate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Face library errors.
var (
	// ErrFaceRecognitionDisabled means Frigate answered 400 "Face recognition is not enabled.".
	ErrFaceRecognitionDisabled = errors.New("face recognition is not enabled in frigate")
	// ErrFaceRenameUnsupported means this Frigate has no PUT /api/faces/{name}/rename route
	// (answers FastAPI's default 404 or 405). The route was verified in the 0.17 source; 0.18
	// was not verified.
	ErrFaceRenameUnsupported = errors.New("this frigate version does not support renaming faces")
	// ErrFaceImageTooLarge means a train attempt exceeds the size cap.
	ErrFaceImageTooLarge = errors.New("face image is too large")
	// ErrInvalidFaceName means a face name is not a Frigate-safe identifier. Names are embedded
	// in request paths and in Frigate's on-disk folder names, so they are validated first.
	ErrInvalidFaceName = errors.New("invalid face name: use 1-50 lowercase letters, digits or underscores")
)

var faceNameRE = regexp.MustCompile(`^[a-z0-9_]{1,50}$`)

// FaceLibrary is what GET /api/faces reports: enrolled image ids per person, and the
// unclassified recognition attempts Frigate keeps under its "train" folder.
type FaceLibrary struct {
	Faces map[string][]string
	Train []string
}

var faceFileRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)

// ValidFaceFile reports whether file is a plain Frigate face image file name (no path parts).
func ValidFaceFile(file string) bool {
	return faceFileRE.MatchString(file) && !strings.Contains(file, "..")
}

func validFaceName(names ...string) error {
	for _, n := range names {
		if !faceNameRE.MatchString(n) {
			return fmt.Errorf("%w: %q", ErrInvalidFaceName, n)
		}
	}
	return nil
}

// faceErr maps Frigate's "face recognition disabled" 400 to its typed error.
func faceErr(err error) error {
	var se *StatusError
	if errors.As(err, &se) && se.Status == http.StatusBadRequest && strings.Contains(se.Body, "Face recognition is not enabled") {
		return fmt.Errorf("%w: %w", ErrFaceRecognitionDisabled, err)
	}
	return err
}

func (a *v017) ListFaces(ctx context.Context) (FaceLibrary, error) {
	var raw map[string][]string
	if err := a.c.getJSON(ctx, "/api/faces", nil, &raw); err != nil {
		return FaceLibrary{}, faceErr(err)
	}
	lib := FaceLibrary{Faces: map[string][]string{}, Train: []string{}}
	for name, ids := range raw {
		sort.Strings(ids)
		if name == "train" {
			lib.Train = ids
			continue
		}
		lib.Faces[name] = ids
	}
	return lib, nil
}

func (a *v017) CreateFace(ctx context.Context, name string) error {
	if err := validFaceName(name); err != nil {
		return err
	}
	_, err := a.c.send(ctx, http.MethodPost, "/api/faces/"+url.PathEscape(name)+"/create", nil, nil)
	return faceErr(err)
}

func (a *v017) RegisterFaceImage(ctx context.Context, name, filename string, data []byte) error {
	if err := validFaceName(name); err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("face image is empty")
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, safeFilename(filename)))
	h.Set("Content-Type", http.DetectContentType(data))
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	_, err = a.c.sendRaw(ctx, http.MethodPost, "/api/faces/"+url.PathEscape(name)+"/register", nil, mw.FormDataContentType(), buf.Bytes())
	return faceErr(err)
}

// safeFilename keeps only the base name and printable ASCII, so a caller-supplied name can
// never inject header content into the multipart part.
func safeFilename(f string) string {
	f = filepath.Base(strings.ReplaceAll(f, "\\", "/"))
	var b strings.Builder
	for _, r := range f {
		if r >= 0x20 && r < 0x7f && r != '"' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 || b.String() == "." || b.String() == "/" {
		return "face.jpg"
	}
	return b.String()
}

func (a *v017) DeleteFaceImages(ctx context.Context, name string, ids []string) error {
	if err := validFaceName(name); err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("no face image ids to delete")
	}
	_, err := a.c.send(ctx, http.MethodPost, "/api/faces/"+url.PathEscape(name)+"/delete", nil, map[string]any{"ids": ids})
	return faceErr(err)
}

func (a *v017) RenameFace(ctx context.Context, oldName, newName string) error {
	if err := validFaceName(oldName, newName); err != nil {
		return err
	}
	_, err := a.c.send(ctx, http.MethodPut, "/api/faces/"+url.PathEscape(oldName)+"/rename", nil, map[string]any{"new_name": newName})
	return classifyRenameErr(err)
}

// classifyRenameErr tells a missing route from a missing source face. A route miss is
// FastAPI's default 404 body ({"detail":"Not Found"}, no "success" key) or a 405; any other
// 404 means the route exists and the source face does not, and stays a plain ErrNotFound.
func classifyRenameErr(err error) error {
	if err == nil {
		return nil
	}
	var nf *NotFoundError
	if errors.As(err, &nf) {
		var body struct {
			Detail  string `json:"detail"`
			Success *bool  `json:"success"`
		}
		if json.Unmarshal([]byte(nf.Body), &body) == nil && body.Success == nil && body.Detail == "Not Found" {
			return fmt.Errorf("%w: %w", ErrFaceRenameUnsupported, err)
		}
		return err
	}
	var se *StatusError
	if errors.As(err, &se) && se.Status == http.StatusMethodNotAllowed {
		return fmt.Errorf("%w: %w", ErrFaceRenameUnsupported, err)
	}
	return faceErr(err)
}

func (a *v017) ClassifyTrainAttempt(ctx context.Context, name, trainingFile string) error {
	if err := validFaceName(name); err != nil {
		return err
	}
	if trainingFile == "" || strings.ContainsAny(trainingFile, `/\`) {
		return errors.New("invalid training file name")
	}
	_, err := a.c.send(ctx, http.MethodPost, "/api/faces/train/"+url.PathEscape(name)+"/classify", nil, map[string]any{"training_file": trainingFile})
	return faceErr(err)
}

const maxFaceImageBytes = 8 << 20

func (a *v017) FaceTrainImage(ctx context.Context, file string) ([]byte, string, error) {
	if !ValidFaceFile(file) {
		return nil, "", fmt.Errorf("%w: %q", ErrInvalidFaceName, file)
	}
	resp, err := a.c.Open(ctx, "/clips/faces/train/"+url.PathEscape(file), nil, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("frigate train image: status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFaceImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > maxFaceImageBytes {
		// Never hand back a silently truncated picture: it would be enrolled as a corrupt image.
		return nil, "", ErrFaceImageTooLarge
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = http.DetectContentType(b)
	}
	return b, ct, nil
}
