package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/ssrf"
)

// Service manages merchant webhook endpoint configuration.
type Service struct {
	repo domainwebhook.Repository
}

// NewService creates a webhook endpoint service.
func NewService(repo domainwebhook.Repository) *Service {
	if repo == nil {
		panic("webhook.NewService: nil repo")
	}
	return &Service{repo: repo}
}

// CreateInput configures a new webhook endpoint.
type CreateInput struct {
	URL         string
	Events      []string
	Active      bool
	Description string
}

// UpdateInput updates an existing webhook endpoint.
type UpdateInput struct {
	ID           string
	URL          string
	Events       []string
	Active       *bool
	Description  string
	RotateSecret bool
}

// EndpointView is the admin-safe endpoint representation.
type EndpointView struct {
	ID           string
	URL          string
	Events       []string
	Active       bool
	Description  string
	SecretPrefix string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CreateResult includes the plaintext secret returned once on create.
type CreateResult struct {
	Endpoint EndpointView
	Secret   string
}

// List returns all configured endpoints without secrets.
func (s *Service) List(ctx context.Context) ([]EndpointView, error) {
	endpoints, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("webhook: list: %w", err)
	}
	out := make([]EndpointView, 0, len(endpoints))
	for i := range endpoints {
		out = append(out, toView(&endpoints[i]))
	}
	return out, nil
}

// Get returns one endpoint without the secret.
func (s *Service) Get(ctx context.Context, endpointID string) (EndpointView, error) {
	ep, err := s.repo.FindByID(ctx, endpointID)
	if err != nil {
		return EndpointView{}, fmt.Errorf("webhook: get: %w", err)
	}
	if ep == nil {
		return EndpointView{}, apperror.NotFound("webhook endpoint not found")
	}
	return toView(ep), nil
}

// Create registers a new endpoint and returns its secret once.
func (s *Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	secret, err := generateSecret()
	if err != nil {
		return nil, fmt.Errorf("webhook: create secret: %w", err)
	}
	ep := &domainwebhook.Endpoint{
		ID:          id.New(),
		URL:         in.URL,
		Secret:      secret,
		Events:      append([]string(nil), in.Events...),
		Active:      in.Active,
		Description: in.Description,
	}
	if err := ep.Validate(domainwebhook.SupportedEventSet()); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := ssrf.ValidateURL(ep.URL); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.repo.Create(ctx, ep); err != nil {
		return nil, fmt.Errorf("webhook: create: %w", err)
	}
	view := toView(ep)
	return &CreateResult{Endpoint: view, Secret: secret}, nil
}

// Update modifies endpoint settings. Secret is unchanged unless RotateSecret is true.
func (s *Service) Update(ctx context.Context, in UpdateInput) (EndpointView, string, error) {
	ep, err := s.repo.FindByID(ctx, in.ID)
	if err != nil {
		return EndpointView{}, "", fmt.Errorf("webhook: update find: %w", err)
	}
	if ep == nil {
		return EndpointView{}, "", apperror.NotFound("webhook endpoint not found")
	}
	ep.URL = in.URL
	ep.Events = append([]string(nil), in.Events...)
	if in.Active != nil {
		ep.Active = *in.Active
	}
	ep.Description = in.Description
	var rotatedSecret string
	if in.RotateSecret {
		rotatedSecret, err = generateSecret()
		if err != nil {
			return EndpointView{}, "", fmt.Errorf("webhook: rotate secret: %w", err)
		}
		ep.Secret = rotatedSecret
	}
	if err := ep.Validate(domainwebhook.SupportedEventSet()); err != nil {
		return EndpointView{}, "", apperror.Validation(err.Error())
	}
	if err := ssrf.ValidateURL(ep.URL); err != nil {
		return EndpointView{}, "", apperror.Validation(err.Error())
	}
	if err := s.repo.Update(ctx, ep); err != nil {
		return EndpointView{}, "", fmt.Errorf("webhook: update: %w", err)
	}
	return toView(ep), rotatedSecret, nil
}

// Delete removes an endpoint.
func (s *Service) Delete(ctx context.Context, endpointID string) error {
	if err := s.repo.Delete(ctx, endpointID); err != nil {
		return fmt.Errorf("webhook: delete: %w", err)
	}
	return nil
}

func toView(ep *domainwebhook.Endpoint) EndpointView {
	return EndpointView{
		ID:           ep.ID,
		URL:          ep.URL,
		Events:       append([]string(nil), ep.Events...),
		Active:       ep.Active,
		Description:  ep.Description,
		SecretPrefix: secretPrefix(ep.Secret),
		CreatedAt:    ep.CreatedAt,
		UpdatedAt:    ep.UpdatedAt,
	}
}

func secretPrefix(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 8 {
		return secret
	}
	return secret[:8]
}

func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
