package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/camconfig/catalog"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

// catalogLabelSource reads the labels the engine model of a server supports; nil result means
// unknown. Implementations check that the actor may view the server.
type catalogLabelSource interface {
	ServerModelLabels(ctx context.Context, a authz.Actor, serverID uuid.UUID) ([]string, error)
}

// GetObjectCatalog serves the normalized object catalog, never an engine's raw label list.
func (h *Handlers) GetObjectCatalog(ctx context.Context, r gen.GetObjectCatalogRequestObject) (gen.GetObjectCatalogResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	engine := catalog.EngineFrigate
	if r.Params.Engine != nil {
		engine = string(*r.Params.Engine)
	}
	var available []string
	if r.Params.ServerId != nil && h.catalogLabels != nil {
		if available, err = h.catalogLabels.ServerModelLabels(ctx, a, *r.Params.ServerId); err != nil {
			return nil, err
		}
	}
	cat := catalog.Default()
	doc := catalogDoc{Engine: engine, Categories: []catalogCategory{}}
	for _, c := range cat.Categories {
		cc := catalogCategory{ID: c.ID, Label: c.Label, Objects: []catalogObject{}}
		for _, o := range c.Objects {
			var sup *bool
			switch catalog.SupportFor(engine, o, available) {
			case catalog.SupportYes:
				t := true
				sup = &t
			case catalog.SupportNo:
				f := false
				sup = &f
			}
			cc.Objects = append(cc.Objects, catalogObject{ID: o.ID, Label: o.Label, RequiresModel: o.RequiresModel, Supported: sup})
		}
		doc.Categories = append(doc.Categories, cc)
	}
	out, err := toGen[gen.ObjectCatalog](doc)
	if err != nil {
		return nil, err
	}
	return gen.GetObjectCatalog200JSONResponse(out), nil
}

// The catalog's JSON shape is the contract; toGen carries it into the generated type.
type catalogDoc struct {
	Engine     string            `json:"engine"`
	Categories []catalogCategory `json:"categories"`
}

type catalogCategory struct {
	ID      string          `json:"id"`
	Label   string          `json:"label"`
	Objects []catalogObject `json:"objects"`
}

type catalogObject struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	RequiresModel bool   `json:"requires_model"`
	Supported     *bool  `json:"supported"`
}

// resolveCatalogSelection turns detection.selection into engine labels using the camera's
// model labels. Ids the model cannot honor reject the apply: nothing is dropped silently.
func (h *Handlers) resolveCatalogSelection(ctx context.Context, a authz.Actor, id uuid.UUID, p camconfig.Patch) (camconfig.Patch, error) {
	if p.Detection == nil || p.Detection.Selection == nil {
		return p, nil
	}
	var src camConfigSource = h.Inv
	if h.camConfig != nil {
		src = h.camConfig
	}
	ce, err := src.OpenCameraEngine(ctx, a, id)
	if err != nil {
		return p, err
	}
	var available []string
	if ce.Adapter != nil {
		available = camconfig.ModelLabels(ctx, ce.Adapter)
	}
	resolved, unsupported := camconfig.ResolveSelection(p, available)
	if len(unsupported) > 0 {
		return p, &inventory.ValidationError{Msg: strings.Join(unsupported, "; ")}
	}
	return resolved, nil
}
