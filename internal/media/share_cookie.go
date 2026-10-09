package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// ShareCookieLifetime is the maximum validity of a public share access cookie.
const ShareCookieLifetime = time.Hour

// ShareCookieName is the cookie that carries proof of a verified share password.
const ShareCookieName = "openvms_share_access"

// ShareCredential carries what a public request presented to prove access to a
// password-protected share: a password (POST only) or a previously issued cookie value.
type ShareCredential struct {
	Password string
	Cookie   string
}

// shareCookieExpiry returns the cookie expiry: one hour from now, never past the share expiry.
func shareCookieExpiry(now time.Time, shareExpiresAt *time.Time) time.Time {
	exp := now.Add(ShareCookieLifetime)
	if shareExpiresAt != nil && shareExpiresAt.Before(exp) {
		exp = *shareExpiresAt
	}
	return exp
}

func shareCookieMAC(pwHash, token string, expUnix int64) string {
	m := hmac.New(sha256.New, []byte(pwHash))
	m.Write([]byte(token + "|" + strconv.FormatInt(expUnix, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// signShareCookie returns "<unix-exp>.<base64url HMAC-SHA256>" keyed by the share's
// password hash, so a changed password invalidates every previously issued cookie.
func signShareCookie(pwHash, token string, exp time.Time) string {
	e := exp.Unix()
	return strconv.FormatInt(e, 10) + "." + shareCookieMAC(pwHash, token, e)
}

// verifyShareCookie checks value against the share token and password hash in constant
// time and rejects expired values. An empty hash (unprotected share) never verifies.
func verifyShareCookie(pwHash, token, value string, now time.Time) bool {
	if pwHash == "" || value == "" {
		return false
	}
	expStr, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	e, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || now.Unix() > e {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(shareCookieMAC(pwHash, token, e)))
}
