package stripepay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/domain/payment"
)

const defaultBaseURL = "https://api.stripe.com"

// Compile-time check that Provider implements payment.Provider.
var _ payment.Provider = (*Provider)(nil)

// Provider implements payment.Provider using the Stripe PaymentIntents API.
// It creates a PaymentIntent and returns a client secret for frontend
// confirmation via Stripe.js.
type Provider struct {
	secretKey string
	baseURL   string
	client    *http.Client
}

// Option configures a Provider.
type Option func(*Provider)

// WithBaseURL overrides the Stripe API base URL (for testing).
func WithBaseURL(u string) Option {
	return func(p *Provider) { p.baseURL = u }
}

// NewProvider creates a Stripe payment provider.
func NewProvider(secretKey string, opts ...Option) *Provider {
	if secretKey == "" {
		panic("stripepay: secret key must not be empty")
	}
	p := &Provider{
		secretKey: secretKey,
		baseURL:   defaultBaseURL,
		client:    &http.Client{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Method returns payment.MethodStripe.
func (p *Provider) Method() payment.PaymentMethod {
	return payment.MethodStripe
}

// paymentIntentResponse is the subset of Stripe's PaymentIntent object
// that we need for initiating a payment.
type paymentIntentResponse struct {
	ID           string `json:"id"`
	ClientSecret string `json:"client_secret"`
	Status       string `json:"status"`
}

// stripeErrorResponse represents a Stripe API error envelope.
type stripeErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Initiate creates a Stripe PaymentIntent for the given payment.
// The payment stays pending; the frontend uses the returned ClientSecret
// with Stripe.js to confirm the payment. A webhook later completes or
// fails the payment.
func (p *Provider) Initiate(ctx context.Context, py *payment.Payment) (payment.ProviderResult, error) {
	if py == nil {
		return payment.ProviderResult{}, fmt.Errorf("stripepay: payment must not be nil")
	}

	form := url.Values{}
	form.Set("amount", strconv.FormatInt(py.Amount.Amount(), 10))
	form.Set("currency", strings.ToLower(py.Amount.Currency()))
	form.Set("metadata[order_id]", py.OrderID)
	form.Set("metadata[payment_id]", py.ID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/payment_intents", strings.NewReader(form.Encode()))
	if err != nil {
		return payment.ProviderResult{}, fmt.Errorf("stripepay: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return payment.ProviderResult{}, fmt.Errorf("stripepay: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return payment.ProviderResult{}, fmt.Errorf("stripepay: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var se stripeErrorResponse
		_ = json.Unmarshal(body, &se)
		if se.Error.Message != "" {
			return payment.ProviderResult{}, fmt.Errorf("stripepay: API error %d: %s", resp.StatusCode, se.Error.Message)
		}
		return payment.ProviderResult{}, fmt.Errorf("stripepay: API error %d", resp.StatusCode)
	}

	var pi paymentIntentResponse
	if err := json.Unmarshal(body, &pi); err != nil {
		return payment.ProviderResult{}, fmt.Errorf("stripepay: parse response: %w", err)
	}

	return payment.ProviderResult{
		ProviderRef:  pi.ID,
		Pending:      true,
		ClientSecret: pi.ClientSecret,
	}, nil
}
