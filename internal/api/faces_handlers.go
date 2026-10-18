package api

import (
	"context"
	"errors"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

// errFacesPending marks the faces endpoints whose handlers land in the next commit.
var errFacesPending = errors.New("faces API not implemented yet")

func (h *Handlers) ListFacePersons(_ context.Context, _ gen.ListFacePersonsRequestObject) (gen.ListFacePersonsResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) CreateFacePerson(_ context.Context, _ gen.CreateFacePersonRequestObject) (gen.CreateFacePersonResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) GetFacePerson(_ context.Context, _ gen.GetFacePersonRequestObject) (gen.GetFacePersonResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) UpdateFacePerson(_ context.Context, _ gen.UpdateFacePersonRequestObject) (gen.UpdateFacePersonResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) DeleteFacePerson(_ context.Context, _ gen.DeleteFacePersonRequestObject) (gen.DeleteFacePersonResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) ListFaceImages(_ context.Context, _ gen.ListFaceImagesRequestObject) (gen.ListFaceImagesResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) UploadFaceImage(_ context.Context, _ gen.UploadFaceImageRequestObject) (gen.UploadFaceImageResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) GetFaceImage(_ context.Context, _ gen.GetFaceImageRequestObject) (gen.GetFaceImageResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) DeleteFaceImage(_ context.Context, _ gen.DeleteFaceImageRequestObject) (gen.DeleteFaceImageResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) ListFaceSyncStatus(_ context.Context, _ gen.ListFaceSyncStatusRequestObject) (gen.ListFaceSyncStatusResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) RetryFaceSync(_ context.Context, _ gen.RetryFaceSyncRequestObject) (gen.RetryFaceSyncResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) ListFaceRecognitions(_ context.Context, _ gen.ListFaceRecognitionsRequestObject) (gen.ListFaceRecognitionsResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) ListFaceTrainAttempts(_ context.Context, _ gen.ListFaceTrainAttemptsRequestObject) (gen.ListFaceTrainAttemptsResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) GetFaceTrainImage(_ context.Context, _ gen.GetFaceTrainImageRequestObject) (gen.GetFaceTrainImageResponseObject, error) {
	return nil, errFacesPending
}

func (h *Handlers) AssignFaceTrainAttempt(_ context.Context, _ gen.AssignFaceTrainAttemptRequestObject) (gen.AssignFaceTrainAttemptResponseObject, error) {
	return nil, errFacesPending
}
