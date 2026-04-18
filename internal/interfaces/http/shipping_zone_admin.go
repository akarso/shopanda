package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ShippingZoneAdminHandler serves admin CRUD for shipping zones and rate tiers.
type ShippingZoneAdminHandler struct {
	zones shipping.ZoneRepository
}

// NewShippingZoneAdminHandler creates a ShippingZoneAdminHandler.
func NewShippingZoneAdminHandler(zones shipping.ZoneRepository) *ShippingZoneAdminHandler {
	if zones == nil {
		panic("ShippingZoneAdminHandler: zone repository must not be nil")
	}
	return &ShippingZoneAdminHandler{zones: zones}
}

// --- Zone request / response types ---

type createZoneRequest struct {
	Name      string   `json:"name"`
	Countries []string `json:"countries"`
	Priority  int      `json:"priority"`
}

type updateZoneRequest struct {
	Name      *string  `json:"name"`
	Countries []string `json:"countries"`
	Priority  *int     `json:"priority"`
	Active    *bool    `json:"active"`
}

type adminZoneResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Countries []string `json:"countries"`
	Priority  int      `json:"priority"`
	Active    bool     `json:"active"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func toAdminZoneResponse(z *shipping.Zone) adminZoneResponse {
	return adminZoneResponse{
		ID:        z.ID,
		Name:      z.Name,
		Countries: z.Countries,
		Priority:  z.Priority,
		Active:    z.Active,
		CreatedAt: z.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: z.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// --- Rate tier request / response types ---

type createRateTierRequest struct {
	MinWeight float64 `json:"min_weight"`
	MaxWeight float64 `json:"max_weight"`
	Price     int64   `json:"price"`
	Currency  string  `json:"currency"`
}

type updateRateTierRequest struct {
	MinWeight *float64 `json:"min_weight"`
	MaxWeight *float64 `json:"max_weight"`
	Price     *int64   `json:"price"`
	Currency  *string  `json:"currency"`
}

type adminRateTierResponse struct {
	ID        string  `json:"id"`
	ZoneID    string  `json:"zone_id"`
	MinWeight float64 `json:"min_weight"`
	MaxWeight float64 `json:"max_weight"`
	Price     int64   `json:"price"`
	Currency  string  `json:"currency"`
}

func toAdminRateTierResponse(rt *shipping.RateTier) adminRateTierResponse {
	return adminRateTierResponse{
		ID:        rt.ID,
		ZoneID:    rt.ZoneID,
		MinWeight: rt.MinWeight,
		MaxWeight: rt.MaxWeight,
		Price:     rt.Price.Amount(),
		Currency:  rt.Price.Currency(),
	}
}

// ---- Zone CRUD ----

// ListZones handles GET /api/v1/admin/shipping/zones.
func (h *ShippingZoneAdminHandler) ListZones() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zones, err := h.zones.ListZones(r.Context())
		if err != nil {
			JSONError(w, err)
			return
		}

		result := make([]adminZoneResponse, 0, len(zones))
		for i := range zones {
			result = append(result, toAdminZoneResponse(&zones[i]))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"zones": result,
		})
	}
}

// CreateZone handles POST /api/v1/admin/shipping/zones.
func (h *ShippingZoneAdminHandler) CreateZone() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createZoneRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		zone, err := shipping.NewZone(id.New(), req.Name, req.Countries, req.Priority)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.zones.CreateZone(r.Context(), &zone); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusCreated, map[string]interface{}{
			"zone": toAdminZoneResponse(&zone),
		})
	}
}

// UpdateZone handles PUT /api/v1/admin/shipping/zones/{id}.
func (h *ShippingZoneAdminHandler) UpdateZone() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zid := r.PathValue("id")
		if zid == "" {
			JSONError(w, apperror.Validation("zone id is required"))
			return
		}

		var req updateZoneRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		zone, err := h.zones.FindZoneByID(r.Context(), zid)
		if err != nil {
			JSONError(w, err)
			return
		}
		if zone == nil {
			JSONError(w, apperror.NotFound("shipping zone not found"))
			return
		}

		if req.Name != nil {
			if *req.Name == "" {
				JSONError(w, apperror.Validation("name must not be empty"))
				return
			}
			zone.Name = *req.Name
		}
		if req.Countries != nil {
			if len(req.Countries) == 0 {
				JSONError(w, apperror.Validation("countries must not be empty"))
				return
			}
			for _, c := range req.Countries {
				if len(c) != 2 {
					JSONError(w, apperror.Validation("country code must be 2 characters: "+c))
					return
				}
			}
			zone.Countries = req.Countries
		}
		if req.Priority != nil {
			zone.Priority = *req.Priority
		}
		if req.Active != nil {
			zone.Active = *req.Active
		}

		zone.UpdatedAt = time.Now().UTC()

		if err := h.zones.UpdateZone(r.Context(), zone); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"zone": toAdminZoneResponse(zone),
		})
	}
}

// DeleteZone handles DELETE /api/v1/admin/shipping/zones/{id}.
func (h *ShippingZoneAdminHandler) DeleteZone() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zid := r.PathValue("id")
		if zid == "" {
			JSONError(w, apperror.Validation("zone id is required"))
			return
		}

		zone, err := h.zones.FindZoneByID(r.Context(), zid)
		if err != nil {
			JSONError(w, err)
			return
		}
		if zone == nil {
			JSONError(w, apperror.NotFound("shipping zone not found"))
			return
		}

		if err := h.zones.DeleteZone(r.Context(), zid); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}

// ---- Rate Tier CRUD ----

// ListRates handles GET /api/v1/admin/shipping/zones/{id}/rates.
func (h *ShippingZoneAdminHandler) ListRates() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zoneID := r.PathValue("id")
		if zoneID == "" {
			JSONError(w, apperror.Validation("zone id is required"))
			return
		}

		// Verify zone exists.
		zone, err := h.zones.FindZoneByID(r.Context(), zoneID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if zone == nil {
			JSONError(w, apperror.NotFound("shipping zone not found"))
			return
		}

		tiers, err := h.zones.ListRateTiers(r.Context(), zoneID)
		if err != nil {
			JSONError(w, err)
			return
		}

		result := make([]adminRateTierResponse, 0, len(tiers))
		for i := range tiers {
			result = append(result, toAdminRateTierResponse(&tiers[i]))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"rates": result,
		})
	}
}

// CreateRate handles POST /api/v1/admin/shipping/zones/{id}/rates.
func (h *ShippingZoneAdminHandler) CreateRate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zoneID := r.PathValue("id")
		if zoneID == "" {
			JSONError(w, apperror.Validation("zone id is required"))
			return
		}

		var req createRateTierRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		if req.Currency == "" {
			req.Currency = "EUR"
		}

		price, err := shared.NewMoney(req.Price, req.Currency)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		rt, err := shipping.NewRateTier(id.New(), zoneID, req.MinWeight, req.MaxWeight, price)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.zones.CreateRateTier(r.Context(), &rt); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusCreated, map[string]interface{}{
			"rate": toAdminRateTierResponse(&rt),
		})
	}
}

// UpdateRate handles PUT /api/v1/admin/shipping/zones/{zoneId}/rates/{rateId}.
func (h *ShippingZoneAdminHandler) UpdateRate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zoneID := r.PathValue("zoneId")
		rateID := r.PathValue("rateId")
		if zoneID == "" || rateID == "" {
			JSONError(w, apperror.Validation("zone id and rate id are required"))
			return
		}

		var req updateRateTierRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		// Fetch existing tiers for this zone to find the one we're updating.
		tiers, err := h.zones.ListRateTiers(r.Context(), zoneID)
		if err != nil {
			JSONError(w, err)
			return
		}

		var tier *shipping.RateTier
		for i := range tiers {
			if tiers[i].ID == rateID {
				tier = &tiers[i]
				break
			}
		}
		if tier == nil {
			JSONError(w, apperror.NotFound("rate tier not found"))
			return
		}

		if req.MinWeight != nil {
			tier.MinWeight = *req.MinWeight
		}
		if req.MaxWeight != nil {
			tier.MaxWeight = *req.MaxWeight
		}
		if req.Price != nil || req.Currency != nil {
			amount := tier.Price.Amount()
			currency := tier.Price.Currency()
			if req.Price != nil {
				amount = *req.Price
			}
			if req.Currency != nil {
				currency = *req.Currency
			}
			price, err := shared.NewMoney(amount, currency)
			if err != nil {
				JSONError(w, apperror.Validation(err.Error()))
				return
			}
			tier.Price = price
		}

		if err := h.zones.UpdateRateTier(r.Context(), tier); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"rate": toAdminRateTierResponse(tier),
		})
	}
}

// DeleteRate handles DELETE /api/v1/admin/shipping/zones/{zoneId}/rates/{rateId}.
func (h *ShippingZoneAdminHandler) DeleteRate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rateID := r.PathValue("rateId")
		if rateID == "" {
			JSONError(w, apperror.Validation("rate id is required"))
			return
		}

		if err := h.zones.DeleteRateTier(r.Context(), rateID); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}
