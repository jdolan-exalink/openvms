package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type requestInfoKey struct{}

type requestInfo struct {
	secure    bool
	userAgent string
}

// withRequestInfo keeps the transport details handlers need to set cookies correctly.
func withRequestInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := requestInfo{secure: secureRequest(r), userAgent: r.UserAgent()}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))
	})
}

func reqInfo(ctx context.Context) requestInfo {
	i, _ := ctx.Value(requestInfoKey{}).(requestInfo)
	return i
}

// #nosec G124 -- Secure intentionally follows HTTPS/TLS detection; forcing it on breaks HTTP deployments.
func sessionCookie(ctx context.Context, token string, expires time.Time) string {
	c := &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: reqInfo(ctx).secure, SameSite: http.SameSiteStrictMode,
	}
	if token == "" {
		c.MaxAge = -1
		c.Expires = time.Time{}
	}
	return c.String()
}

func (h *Handlers) me(_ context.Context, u db.User, grants []db.PermissionGrant, method gen.MeAuthMethod) gen.Me {
	return gen.Me{
		Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, TenantId: u.TenantID,
		Grants: toGrants(grants), MfaEnabled: u.MfaEnabled, MustChangePassword: u.MustChangePassword,
		AuthMethod: method,
	}
}

func (h *Handlers) Login(ctx context.Context, r gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	b := r.Body
	res, err := h.Identity.Login(ctx, identity.LoginInput{
		Username: b.Username, Password: b.Password, TOTP: deref(b.TotpCode),
		IP: httpx.ClientIP(ctx), UserAgent: reqInfo(ctx).userAgent,
	})
	switch {
	case errors.Is(err, identity.ErrLocked):
		return gen.Login429JSONResponse{Code: "locked", Message: "Demasiados intentos fallidos. Probá de nuevo en unos minutos."}, nil
	case errors.Is(err, identity.ErrMFAInvalid):
		return gen.Login401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse{Code: "mfa_invalid", Message: "El código de verificación no es válido."}}, nil
	case errors.Is(err, identity.ErrInvalidCredentials):
		return gen.Login401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse{Code: "invalid_credentials", Message: "Usuario o contraseña incorrectos."}}, nil
	case err != nil:
		return nil, err
	}
	if res.MFARequired {
		return gen.Login200JSONResponse{Body: gen.LoginResponse{MfaRequired: true}}, nil
	}
	actor := actorOf(res.User)
	u, grants, err := h.Inv.Me(ctx, actor)
	if err != nil {
		return nil, err
	}
	me := h.me(ctx, u, grants, gen.Session)
	cookie := sessionCookie(ctx, res.Token, res.ExpiresAt)
	return gen.Login200JSONResponse{
		Body:    gen.LoginResponse{MfaRequired: false, ExpiresAt: &res.ExpiresAt, User: &me},
		Headers: gen.Login200ResponseHeaders{SetCookie: &cookie},
	}, nil
}

func (h *Handlers) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if tok := sessionToken(ctx); tok != "" {
		if err := h.Identity.Logout(ctx, a, tok); err != nil {
			return nil, err
		}
	}
	cookie := sessionCookie(ctx, "", time.Time{})
	return gen.Logout204Response{Headers: gen.Logout204ResponseHeaders{SetCookie: &cookie}}, nil
}

func (h *Handlers) ChangePassword(ctx context.Context, r gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Identity.ChangePassword(ctx, a, r.Body.CurrentPassword, r.Body.NewPassword); err != nil {
		return nil, err
	}
	return gen.ChangePassword204Response{}, nil
}

func (h *Handlers) SetupMfa(ctx context.Context, _ gen.SetupMfaRequestObject) (gen.SetupMfaResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	secret, url, err := h.Identity.SetupMFA(ctx, a)
	if err != nil {
		return nil, err
	}
	return gen.SetupMfa200JSONResponse{Secret: secret, OtpauthUrl: url}, nil
}

func (h *Handlers) EnableMfa(ctx context.Context, r gen.EnableMfaRequestObject) (gen.EnableMfaResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Identity.EnableMFA(ctx, a, r.Body.Code); err != nil {
		return nil, err
	}
	return gen.EnableMfa204Response{}, nil
}

func (h *Handlers) DisableMfa(ctx context.Context, r gen.DisableMfaRequestObject) (gen.DisableMfaResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Identity.DisableMFA(ctx, a, r.Body.Password); err != nil {
		return nil, err
	}
	return gen.DisableMfa204Response{}, nil
}

func toUser(u identity.User) gen.User {
	return gen.User{
		Id: u.ID, TenantId: u.TenantID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email,
		Status: gen.UserStatus(u.Status), MfaEnabled: u.MfaEnabled, HasPassword: u.PasswordHash != "",
		MustChangePassword: u.MustChangePassword, LockedUntil: u.LockedUntil, LastLoginAt: u.LastLoginAt,
		GroupIds: u.GroupIDs, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func (h *Handlers) ListUsers(ctx context.Context, r gen.ListUsersRequestObject) (gen.ListUsersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	us, err := h.Identity.ListUsers(ctx, a, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	out := gen.ListUsers200JSONResponse{Items: make([]gen.User, 0, len(us))}
	for _, u := range us {
		out.Items = append(out.Items, toUser(u))
	}
	return out, nil
}

func (h *Handlers) CreateUser(ctx context.Context, r gen.CreateUserRequestObject) (gen.CreateUserResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	u, err := h.Identity.CreateUser(ctx, a, identity.CreateUserInput{
		TenantID: b.TenantId, Username: b.Username, DisplayName: deref(b.DisplayName), Email: b.Email,
		Password: deref(b.Password), MustChange: deref(b.MustChangePassword), GroupIDs: uuids(b.GroupIds),
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateUser201JSONResponse(toUser(u)), nil
}

func (h *Handlers) GetUser(ctx context.Context, r gen.GetUserRequestObject) (gen.GetUserResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	u, err := h.Identity.GetUser(ctx, a, r.UserId)
	if err != nil {
		return nil, err
	}
	return gen.GetUser200JSONResponse(toUser(u)), nil
}

func (h *Handlers) UpdateUser(ctx context.Context, r gen.UpdateUserRequestObject) (gen.UpdateUserResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	in := identity.UpdateUserInput{
		DisplayName: b.DisplayName, Email: b.Email, Password: b.Password,
		MustChange: deref(b.MustChangePassword), GroupIDs: b.GroupIds, DisableMFA: deref(b.DisableMfa),
	}
	if b.Status != nil {
		s := string(*b.Status)
		in.Status = &s
	}
	u, err := h.Identity.UpdateUser(ctx, a, r.UserId, in)
	if err != nil {
		return nil, err
	}
	return gen.UpdateUser200JSONResponse(toUser(u)), nil
}

func (h *Handlers) DeleteUser(ctx context.Context, r gen.DeleteUserRequestObject) (gen.DeleteUserResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Identity.DeleteUser(ctx, a, r.UserId); err != nil {
		return nil, err
	}
	return gen.DeleteUser204Response{}, nil
}

func toUserGroup(g identity.Group) gen.UserGroup {
	return gen.UserGroup{
		Id: g.ID, TenantId: g.TenantID, Name: g.Name, Description: g.Description,
		MemberIds: g.MemberIDs, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
}

func (h *Handlers) ListUserGroups(ctx context.Context, r gen.ListUserGroupsRequestObject) (gen.ListUserGroupsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	gs, err := h.Identity.ListGroups(ctx, a, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	out := gen.ListUserGroups200JSONResponse{Items: make([]gen.UserGroup, 0, len(gs))}
	for _, g := range gs {
		out.Items = append(out.Items, toUserGroup(g))
	}
	return out, nil
}

func userGroupInput(b *gen.UserGroupInput) identity.GroupInput {
	return identity.GroupInput{TenantID: b.TenantId, Name: b.Name, Description: deref(b.Description), MemberIDs: uuids(b.MemberIds)}
}

func (h *Handlers) CreateUserGroup(ctx context.Context, r gen.CreateUserGroupRequestObject) (gen.CreateUserGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	g, err := h.Identity.CreateGroup(ctx, a, userGroupInput(r.Body))
	if err != nil {
		return nil, err
	}
	return gen.CreateUserGroup201JSONResponse(toUserGroup(g)), nil
}

func (h *Handlers) GetUserGroup(ctx context.Context, r gen.GetUserGroupRequestObject) (gen.GetUserGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	g, err := h.Identity.GetGroup(ctx, a, r.GroupId)
	if err != nil {
		return nil, err
	}
	return gen.GetUserGroup200JSONResponse(toUserGroup(g)), nil
}

func (h *Handlers) ReplaceUserGroup(ctx context.Context, r gen.ReplaceUserGroupRequestObject) (gen.ReplaceUserGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	g, err := h.Identity.ReplaceGroup(ctx, a, r.GroupId, userGroupInput(r.Body))
	if err != nil {
		return nil, err
	}
	return gen.ReplaceUserGroup200JSONResponse(toUserGroup(g)), nil
}

func (h *Handlers) DeleteUserGroup(ctx context.Context, r gen.DeleteUserGroupRequestObject) (gen.DeleteUserGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Identity.DeleteGroup(ctx, a, r.GroupId); err != nil {
		return nil, err
	}
	return gen.DeleteUserGroup204Response{}, nil
}

func (h *Handlers) ListAudit(ctx context.Context, r gen.ListAuditRequestObject) (gen.ListAuditResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p := r.Params
	rows, err := h.Identity.ListAudit(ctx, a, identity.AuditFilter{
		TenantID: p.TenantId, Action: p.Action, ActorID: p.ActorId, From: p.From, To: p.To,
		BeforeID: p.BeforeId, Limit: deref(p.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := gen.ListAudit200JSONResponse{Items: make([]gen.AuditEntry, 0, len(rows))}
	for _, e := range rows {
		details := map[string]any{}
		_ = json.Unmarshal(e.Details, &details)
		var ip *string
		if e.Ip != nil {
			s := e.Ip.String()
			ip = &s
		}
		out.Items = append(out.Items, gen.AuditEntry{
			Id: e.ID, OccurredAt: e.OccurredAt, TenantId: e.TenantID, ActorId: e.ActorID, ActorName: e.ActorName,
			Action: e.Action, TargetType: e.TargetType, TargetId: e.TargetID, RequestId: e.RequestID, Ip: ip, Details: details,
		})
	}
	return out, nil
}

func actorOf(u db.User) authz.Actor {
	return authz.Actor{UserID: u.ID, Username: u.Username, TenantID: u.TenantID}
}
