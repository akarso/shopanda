package extref

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// DesignLineRequest is the JSON body for POST /extref/design-line.
type DesignLineRequest struct {
	VariantID string `json:"variant_id"`
	DesignID  string `json:"design_id"`
	Quantity  int    `json:"quantity"`
}

// DesignLineResponse is returned without a cart_id (session stays in the cookie).
type DesignLineResponse struct {
	Status    string `json:"status"` // "added" | "unchanged" | "attached"
	VariantID string `json:"variant_id"`
	DesignID  string `json:"design_id"`
}

// designLineHandler demonstrates the design-on-line pattern: EnsureStorefrontCart,
// idempotent same-design, 409 when another design already owns the line, and
// attach-without-qty-bump when the variant line already exists.
type designLineHandler struct {
	host pluginsdk.Host
}

func (h *designLineHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.host == nil {
		http.Error(w, "extref: host not configured", http.StatusServiceUnavailable)
		return
	}
	port, ok := h.host.Cart()
	if !ok || port == nil {
		http.Error(w, "extref: cart port not bound", http.StatusServiceUnavailable)
		return
	}

	var req DesignLineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	req.VariantID = strings.TrimSpace(req.VariantID)
	req.DesignID = strings.TrimSpace(req.DesignID)
	if req.VariantID == "" || req.DesignID == "" {
		http.Error(w, "variant_id and design_id are required", http.StatusBadRequest)
		return
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	// Blank currency → store on request context, else EUR (same as storefront).
	cart, err := port.EnsureStorefrontCart(r.Context(), w, r, "")
	if err != nil {
		writeDesignLineErr(w, err)
		return
	}
	if cart == nil {
		http.Error(w, "cart unavailable", http.StatusInternalServerError)
		return
	}

	for _, line := range cart.Items {
		if line.VariantID != req.VariantID {
			continue
		}
		existing := lineDesignID(line)
		if existing == req.DesignID {
			writeDesignLineJSON(w, http.StatusOK, DesignLineResponse{
				Status:    "unchanged",
				VariantID: req.VariantID,
				DesignID:  req.DesignID,
			})
			return
		}
		if existing != "" {
			writeDesignLineJSON(w, http.StatusConflict, map[string]string{
				"error":      "design_conflict",
				"variant_id": req.VariantID,
				"design_id":  existing,
			})
			return
		}
		// Line exists without design_id — attach without increasing quantity.
		ctx := h.host.WithPluginWrite(r.Context(), PluginName, FieldCodeDesignID)
		if err := port.UpsertLineExtension(ctx, cart.ID, cart.CustomerID, req.VariantID, []pluginsdk.CartExtensionValue{
			{Code: FieldCodeDesignID, Value: req.DesignID},
		}, "plugin:"+PluginName); err != nil {
			writeDesignLineErr(w, err)
			return
		}
		writeDesignLineJSON(w, http.StatusOK, DesignLineResponse{
			Status:    "attached",
			VariantID: req.VariantID,
			DesignID:  req.DesignID,
		})
		return
	}

	ctx := h.host.WithPluginWrite(r.Context(), PluginName, FieldCodeDesignID)
	if err := port.AddItem(ctx, cart.ID, cart.CustomerID, req.VariantID, req.Quantity, pluginsdk.CartAddItemOpts{
		Extensions: []pluginsdk.CartExtensionValue{{Code: FieldCodeDesignID, Value: req.DesignID}},
		UpdatedBy:  "plugin:" + PluginName,
	}); err != nil {
		writeDesignLineErr(w, err)
		return
	}
	writeDesignLineJSON(w, http.StatusOK, DesignLineResponse{
		Status:    "added",
		VariantID: req.VariantID,
		DesignID:  req.DesignID,
	})
}

func lineDesignID(line extapi.CartLineView) string {
	for _, e := range line.Extensions {
		if e.Code == FieldCodeDesignID {
			if s, ok := e.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

func writeDesignLineJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeDesignLineErr(w http.ResponseWriter, err error) {
	if err == nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	switch {
	case pluginsdk.IsNotFound(err):
		http.Error(w, "not found", http.StatusNotFound)
	case pluginsdk.IsForbidden(err):
		http.Error(w, "forbidden", http.StatusForbidden)
	case pluginsdk.IsConflict(err):
		http.Error(w, "conflict", http.StatusConflict)
	case pluginsdk.IsValidation(err):
		http.Error(w, "validation failed", http.StatusBadRequest)
	default:
		http.Error(w, "cart unavailable", http.StatusInternalServerError)
	}
}

func newDesignLineHandler(h pluginsdk.Host) (http.Handler, error) {
	if h == nil {
		return nil, fmt.Errorf("extref: host not configured")
	}
	// CartPort is late-bound after Init on serve. Fail-closed per request when
	// Cart() is missing — registration itself cannot require a bound port.
	return &designLineHandler{host: h}, nil
}

// DesignLineHandlerForTest exposes the design-line handler for package tests.
func DesignLineHandlerForTest(h pluginsdk.Host) (http.Handler, error) {
	return newDesignLineHandler(h)
}
