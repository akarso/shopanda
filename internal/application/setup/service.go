package setup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/seed"
)

// setupInstallAdvisoryLockKey serializes concurrent web install requests.
const setupInstallAdvisoryLockKey int64 = 903001

// AdminChecker reports whether the store already has an active admin account.
type AdminChecker interface {
	HasActiveAdmin(ctx context.Context) (bool, error)
}

// StoreUpdater optionally renames the default store during first install.
type StoreUpdater interface {
	FindDefault(ctx context.Context) (*store.Store, error)
	Update(ctx context.Context, s *store.Store) error
}

// SeedRunner executes default seeders (migrate is handled separately).
type SeedRunner func(ctx context.Context, deps seed.Deps) (*seed.Result, error)

// AdminUserCreator provisions admin-capable accounts during first install.
type AdminUserCreator interface {
	Create(ctx context.Context, in AdminUserCreateInput) (*customer.Customer, error)
}

// AdminUserCreateInput is the data required to create the first admin user.
type AdminUserCreateInput struct {
	Email     string
	Password  string
	FirstName string
	LastName  string
	Role      customer.Role
}

// Service orchestrates first-time web installation.
type Service struct {
	db            *sql.DB
	migrationsDir string
	admins        AdminChecker
	stores        StoreUpdater
	adminUsers    AdminUserCreator
	runSeed       SeedRunner
	log           logger.Logger
}

// NewService creates a setup service.
func NewService(
	db *sql.DB,
	migrationsDir string,
	admins AdminChecker,
	stores StoreUpdater,
	adminUsers AdminUserCreator,
	runSeed SeedRunner,
	log logger.Logger,
) *Service {
	if db == nil {
		panic("setup.NewService: nil db")
	}
	if migrationsDir == "" {
		panic("setup.NewService: empty migrations dir")
	}
	if admins == nil {
		panic("setup.NewService: nil admin checker")
	}
	if adminUsers == nil {
		panic("setup.NewService: nil admin user service")
	}
	if runSeed == nil {
		panic("setup.NewService: nil seed runner")
	}
	if log == nil {
		panic("setup.NewService: nil logger")
	}
	return &Service{
		db:            db,
		migrationsDir: migrationsDir,
		admins:        admins,
		stores:        stores,
		adminUsers:    adminUsers,
		runSeed:       runSeed,
		log:           log,
	}
}

// Status summarizes whether the web installer should run.
type Status struct {
	NeedsSetup        bool `json:"needs_setup"`
	DatabaseOK        bool `json:"database_ok"`
	PendingMigrations int  `json:"pending_migrations"`
	HasAdmin          bool `json:"has_admin"`
}

// InstallInput is the merchant-provided first-boot data.
type InstallInput struct {
	Email     string
	Password  string
	FirstName string
	LastName  string
	StoreName string
}

// InstallResult summarizes a successful installation.
type InstallResult struct {
	AdminEmail       string `json:"admin_email"`
	MigrationsApplied int    `json:"migrations_applied"`
}

// Status inspects database connectivity, migrations, and admin presence.
func (s *Service) Status(ctx context.Context) (Status, error) {
	out := Status{NeedsSetup: true}

	if err := s.db.PingContext(ctx); err != nil {
		return out, nil
	}
	out.DatabaseOK = true

	pending, err := migrate.PendingCount(s.db, s.migrationsDir)
	if err != nil {
		return Status{}, fmt.Errorf("setup status: pending migrations: %w", err)
	}
	out.PendingMigrations = pending

	hasAdmin, err := s.admins.HasActiveAdmin(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("setup status: admin check: %w", err)
	}
	out.HasAdmin = hasAdmin
	out.NeedsSetup = !hasAdmin
	return out, nil
}

// Install applies migrations, seeds defaults, and creates the first admin user.
// It is only allowed while no active admin account exists.
func (s *Service) Install(ctx context.Context, in InstallInput) (*InstallResult, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !status.DatabaseOK {
		return nil, apperror.Validation("database is not reachable")
	}
	if status.HasAdmin {
		return nil, apperror.Conflict("store is already installed")
	}

	var result *InstallResult
	err = s.withInstallLock(ctx, func(ctx context.Context) error {
		hasAdmin, err := s.admins.HasActiveAdmin(ctx)
		if err != nil {
			return fmt.Errorf("setup install: admin check: %w", err)
		}
		if hasAdmin {
			return apperror.Conflict("store is already installed")
		}

		applied, err := migrate.Run(s.db, s.migrationsDir)
		if err != nil {
			return fmt.Errorf("setup install: migrate: %w", err)
		}

		deps := seed.Deps{DB: s.db, Logger: s.log}
		if _, err := s.runSeed(ctx, deps); err != nil {
			return fmt.Errorf("setup install: seed: %w", err)
		}

		hasAdmin, err = s.admins.HasActiveAdmin(ctx)
		if err != nil {
			return fmt.Errorf("setup install: admin check: %w", err)
		}
		if !hasAdmin {
			admin, err := s.adminUsers.Create(ctx, AdminUserCreateInput{
				Email:     in.Email,
				Password:  in.Password,
				FirstName: in.FirstName,
				LastName:  in.LastName,
				Role:      customer.RoleAdmin,
			})
			if err != nil {
				return err
			}
			in.Email = admin.Email
		}

		if name := strings.TrimSpace(in.StoreName); name != "" && s.stores != nil {
			if err := s.renameDefaultStore(ctx, name); err != nil {
				s.log.Warn("setup.install.store_rename_failed", map[string]interface{}{
					"error": err.Error(),
				})
			}
		}

		result = &InstallResult{
			AdminEmail:        in.Email,
			MigrationsApplied: applied,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) withInstallLock(ctx context.Context, fn func(context.Context) error) error {
	if _, err := s.db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", setupInstallAdvisoryLockKey); err != nil {
		return fmt.Errorf("setup install: acquire lock: %w", err)
	}
	defer func() {
		_, _ = s.db.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", setupInstallAdvisoryLockKey)
	}()
	return fn(ctx)
}

func (s *Service) renameDefaultStore(ctx context.Context, name string) error {
	st, err := s.stores.FindDefault(ctx)
	if err != nil {
		return err
	}
	if st == nil {
		return nil
	}
	st.Name = name
	return s.stores.Update(ctx, st)
}
