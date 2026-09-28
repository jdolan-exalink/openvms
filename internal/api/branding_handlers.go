package api

import (
	"bytes"
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/branding"
)

func toTenantBranding(b branding.Branding) gen.TenantBranding {
	out := gen.TenantBranding{TenantId: b.TenantID, OwnerName: b.OwnerName, HasLogo: b.HasLogo, UpdatedAt: b.UpdatedAt}
	if b.LogoContentType != "" {
		out.LogoContentType = &b.LogoContentType
	}
	return out
}

func (h *Handlers) GetTenantBranding(ctx context.Context, r gen.GetTenantBrandingRequestObject) (gen.GetTenantBrandingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b, err := h.Branding.Get(ctx, a, r.TenantId)
	if err != nil {
		return nil, err
	}
	return gen.GetTenantBranding200JSONResponse(toTenantBranding(b)), nil
}

func (h *Handlers) UpdateTenantBranding(ctx context.Context, r gen.UpdateTenantBrandingRequestObject) (gen.UpdateTenantBrandingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	body := r.Body
	if body == nil {
		body = &gen.UpdateTenantBrandingJSONRequestBody{}
	}
	in := branding.Input{OwnerName: body.OwnerName, RemoveLogo: body.RemoveLogo != nil && *body.RemoveLogo}
	if body.Logo != nil {
		in.Logo = *body.Logo
	}
	if body.LogoContentType != nil {
		in.LogoContentType = string(*body.LogoContentType)
	}
	b, err := h.Branding.Update(ctx, a, r.TenantId, in)
	if err != nil {
		return nil, err
	}
	return gen.UpdateTenantBranding200JSONResponse(toTenantBranding(b)), nil
}

func (h *Handlers) DeleteTenantBranding(ctx context.Context, r gen.DeleteTenantBrandingRequestObject) (gen.DeleteTenantBrandingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Branding.Delete(ctx, a, r.TenantId); err != nil {
		return nil, err
	}
	return gen.DeleteTenantBranding204Response{}, nil
}

func (h *Handlers) GetTenantBrandingLogo(ctx context.Context, r gen.GetTenantBrandingLogoRequestObject) (gen.GetTenantBrandingLogoResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	data, contentType, err := h.Branding.Logo(ctx, a, r.TenantId)
	if err != nil {
		return nil, err
	}
	return gen.GetTenantBrandingLogo200ImageResponse{Body: bytes.NewReader(data), ContentType: contentType, ContentLength: int64(len(data))}, nil
}
