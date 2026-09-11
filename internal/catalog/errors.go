package catalog

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound                     = errors.New("catalog entity not found")
	ErrNameConflict                 = errors.New("catalog name conflict")
	ErrRequestConflict              = errors.New("request conflict")
	ErrInvalidPricingConfiguration  = errors.New("invalid pricing configuration")
	ErrInvalidModifierConfiguration = errors.New("invalid modifier configuration")
	ErrInvalidInheritance           = errors.New("invalid inheritance")
	ErrEntityRetired                = errors.New("entity retired")
	ErrInvalidManagerPin            = errors.New("invalid manager pin")
	ErrForbidden                    = errors.New("forbidden")
	ErrUnauthorized                 = errors.New("unauthorized")
	ErrInvalidStoredResult          = errors.New("invalid stored result")
)

// MapDBError maps PostgreSQL driver and database errors to domain sentinels.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, err.Error())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			msg := pgErr.Detail
			if msg == "" {
				msg = pgErr.Message
			}
			return fmt.Errorf("%w: %s", ErrNameConflict, msg)
		case "23503": // foreign_key_violation
			msg := pgErr.Detail
			if msg == "" {
				msg = pgErr.Message
			}
			return fmt.Errorf("%w: %s", ErrNotFound, msg)
		}
	}
	return err
}

