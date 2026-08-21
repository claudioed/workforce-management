package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/associate"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// AssociateRepo is a pgxpool-backed ports.AssociateRepo.
type AssociateRepo struct {
	pool *pgxpool.Pool
}

// NewAssociateRepo constructs an AssociateRepo backed by pool.
func NewAssociateRepo(pool *pgxpool.Pool) *AssociateRepo {
	return &AssociateRepo{pool: pool}
}

// Save upserts a's current state.
func (r *AssociateRepo) Save(ctx context.Context, a *associate.AssociateShift) error {
	certs := certsToStrings(a.Certifications())
	_, err := r.pool.Exec(ctx, `
		INSERT INTO associate_shift (associate_id, certifications, on_break, hours_logged, ended)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (associate_id) DO UPDATE SET
			certifications = EXCLUDED.certifications,
			on_break = EXCLUDED.on_break,
			hours_logged = EXCLUDED.hours_logged,
			ended = EXCLUDED.ended
	`, string(a.AssociateId()), certs, a.IsOnBreak(), a.HoursLogged(), a.Ended())
	return err
}

// FindByID loads the AssociateShift for id, or ports.ErrNotFound.
func (r *AssociateRepo) FindByID(ctx context.Context, id shared.AssociateId) (*associate.AssociateShift, error) {
	var certs []string
	var onBreak, ended bool
	var hoursLogged float64

	row := r.pool.QueryRow(ctx, `
		SELECT certifications, on_break, hours_logged, ended
		FROM associate_shift WHERE associate_id = $1
	`, string(id))

	if err := row.Scan(&certs, &onBreak, &hoursLogged, &ended); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrNotFound
		}
		return nil, err
	}

	return associate.Rehydrate(id, stringsToCerts(certs), onBreak, hoursLogged, ended), nil
}

func certsToStrings(certs []shared.Certification) []string {
	out := make([]string, len(certs))
	for i, c := range certs {
		out[i] = string(c)
	}
	return out
}

func stringsToCerts(strs []string) []shared.Certification {
	out := make([]shared.Certification, len(strs))
	for i, s := range strs {
		out[i] = shared.Certification(s)
	}
	return out
}
