package infra

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/huncoding/payment-processor-course/migrations"
	"github.com/pressly/goose/v3"
)

// Migrate applies pending migrations at service startup. Embedding the SQL files in the binary means the 
// deployed artifact and its schema can never drift apart.
func Migrate(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("migrate: failed to open database: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("migrate: failed to set dialect: %w", err)
	}

	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("migrate: failed to migrate: %w", err)
	}

	return nil
}
