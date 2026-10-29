package storefront

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	downloadapp "github.com/akarso/shopanda/internal/application/download"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/media"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// DefaultDownloadSignedURLTTL is the short-lived redirect window for S3
// grant delivery. Keep short so expiry/limit remain enforceable.
const DefaultDownloadSignedURLTTL = 60 * time.Second

// signedURLHostAllowlist is optionally implemented by storage backends that
// know which hosts their SignedURL responses may redirect to.
type signedURLHostAllowlist interface {
	SignedURLHosts() []string
}

// DownloadAccountHandler is the storefront download delivery API (PR-1077).
// Admin grant list/revoke/re-issue is PR-1078.
type DownloadAccountHandler struct {
	grants  *downloadapp.Service
	assets  media.AssetRepository
	storage media.Storage
	ttl     time.Duration
	log     logger.Logger
}

// NewDownloadAccountHandler creates a DownloadAccountHandler.
// log may be nil (errors are still returned to the client).
func NewDownloadAccountHandler(grants *downloadapp.Service, assets media.AssetRepository, storage media.Storage, log logger.Logger) *DownloadAccountHandler {
	if grants == nil {
		panic("http: download grant service must not be nil")
	}
	if assets == nil {
		panic("http: asset repository must not be nil")
	}
	if storage == nil {
		panic("http: media storage must not be nil")
	}
	return &DownloadAccountHandler{
		grants:  grants,
		assets:  assets,
		storage: storage,
		ttl:     DefaultDownloadSignedURLTTL,
		log:     log,
	}
}

// Deliver handles POST /api/v1/account/downloads/{grantID}.
// Auth is enforced by requireAuth at the route. Ownership, expiry, and
// download limit are checked; the object is proven to exist (Head/Read)
// and a signed URL or stream is prepared before ClaimDelivery increments
// the count. Permanent storage.URL() is never returned.
func (h *DownloadAccountHandler) Deliver() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		grantID := strings.TrimSpace(r.PathValue("grantID"))
		if grantID == "" {
			httpshared.JSONError(w, apperror.Validation("download grant id is required"))
			return
		}

		g, err := h.grants.PrepareDelivery(r.Context(), customerID, grantID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		asset, err := h.assets.FindByID(r.Context(), g.AssetID)
		if err != nil {
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "load download asset failed", err))
			return
		}
		if asset == nil {
			httpshared.JSONError(w, apperror.NotFound("download asset not found"))
			return
		}

		ttl := signedTTLForGrant(h.ttl, time.Now().UTC(), g)

		// Prefer short-lived signed redirect when the backend supports it.
		if signer, ok := h.storage.(media.SignedURLStorage); ok {
			if err := signer.Head(r.Context(), asset.Path); err != nil {
				if media.IsNotFound(err) {
					httpshared.JSONError(w, apperror.NotFound("download file not found in storage"))
					return
				}
				h.logDeliveryError("storefront.download.delivery.head_failed", err, grantID, asset.ID)
				httpshared.JSONError(w, apperror.Internal("download delivery failed"))
				return
			}
			signed, err := signer.SignedURL(r.Context(), asset.Path, media.SignURLOptions{
				TTL:      ttl,
				Filename: asset.Filename,
			})
			if err != nil {
				h.logDeliveryError("storefront.download.delivery.presign_failed", err, grantID, asset.ID)
				httpshared.JSONError(w, apperror.Internal("download delivery failed"))
				return
			}
			if err := validateSignedRedirectURL(signed, h.storage); err != nil {
				h.logDeliveryError("storefront.download.delivery.signed_url_invalid", err, grantID, asset.ID)
				httpshared.JSONError(w, apperror.Internal("download delivery failed"))
				return
			}
			if _, err := h.grants.ClaimDelivery(r.Context(), customerID, g.ID); err != nil {
				httpshared.JSONError(w, err)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			http.Redirect(w, r, signed, http.StatusFound)
			return
		}

		reader, ok := h.storage.(media.ReadableStorage)
		if !ok {
			httpshared.JSONError(w, apperror.Wrap(
				apperror.CodeInternal,
				"download delivery is not configured for this storage backend",
				fmt.Errorf("storage %T supports neither SignedURL nor Read", h.storage),
			))
			return
		}
		data, err := reader.Read(asset.Path)
		if err != nil {
			if media.IsNotFound(err) {
				httpshared.JSONError(w, apperror.NotFound("download file not found in storage"))
				return
			}
			h.logDeliveryError("storefront.download.delivery.read_failed", err, grantID, asset.ID)
			httpshared.JSONError(w, apperror.Internal("download delivery failed"))
			return
		}

		if _, err := h.grants.ClaimDelivery(r.Context(), customerID, g.ID); err != nil {
			httpshared.JSONError(w, err)
			return
		}

		filename := media.SanitizeContentDispositionFilename(asset.Filename)
		mimeType := strings.TrimSpace(asset.MimeType)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = w.Write(data)
	}
}

func (h *DownloadAccountHandler) logDeliveryError(event string, err error, grantID, assetID string) {
	if h == nil || h.log == nil {
		return
	}
	h.log.Error(event, err, map[string]interface{}{
		"grant_id": grantID,
		"asset_id": assetID,
	})
}

func signedTTLForGrant(configured time.Duration, now time.Time, g *domain.DownloadGrant) time.Duration {
	ttl := configured
	if ttl <= 0 {
		ttl = DefaultDownloadSignedURLTTL
	}
	if g == nil || g.ExpiresAt.IsZero() {
		return ttl
	}
	remaining := g.ExpiresAt.UTC().Sub(now.UTC())
	if remaining <= 0 {
		return time.Second // PrepareDelivery should have rejected; keep positive for signer
	}
	if remaining < ttl {
		return remaining
	}
	return ttl
}

// validateSignedRedirectURL rejects empty, non-http(s), CRLF-bearing, or
// off-host URLs before Redirect. http is allowed only for storage name "s3"
// (local MinIO). Host must match the storage endpoint/base allowlist.
func validateSignedRedirectURL(raw string, storage media.Storage) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty signed URL")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("signed URL contains CR/LF")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse signed URL: %w", err)
	}
	storageName := ""
	if storage != nil {
		storageName = storage.Name()
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		// ok
	case "http":
		// Local MinIO / path-style S3-compatible endpoints often use http.
		if storageName != "s3" {
			return fmt.Errorf("signed URL scheme %q not allowed", u.Scheme)
		}
	default:
		return fmt.Errorf("signed URL scheme %q not allowed", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("signed URL missing host")
	}
	allowed := signedRedirectAllowedHosts(storage)
	if len(allowed) == 0 {
		return fmt.Errorf("signed URL host allowlist empty")
	}
	for _, a := range allowed {
		if host == strings.ToLower(a) {
			return nil
		}
	}
	return fmt.Errorf("signed URL host %q not allowed", host)
}

func signedRedirectAllowedHosts(storage media.Storage) []string {
	if storage == nil {
		return nil
	}
	if a, ok := storage.(signedURLHostAllowlist); ok {
		return a.SignedURLHosts()
	}
	if u, err := url.Parse(storage.URL(".")); err == nil {
		if h := u.Hostname(); h != "" {
			return []string{h}
		}
	}
	return nil
}
