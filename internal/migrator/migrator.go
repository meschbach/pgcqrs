package migrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/meschbach/pgcqrs/migrations"
)

func MigratePrimary(ctx context.Context, config Config) (problem error) {
	migrationsFS, err := iofs.New(migrations.Primary, "primary")
	if err != nil {
		return err
	}
	db := "pgx://" + config.Storage.Primary.DatabaseURL

	migrator, err := migrate.NewWithSourceInstance("primary", migrationsFS, db)
	if err != nil {
		fmt.Println("Migration creation failed")
		return err
	}
	defer func() {
		sourceError, destinationError := migrator.Close()
		problem = errors.Join(problem, sourceError, destinationError)
	}()
	migrator.Log = &migratorLogger{}
	if err := migrator.Up(); err != nil {
		if err.Error() != "no change" {
			return err
		}
	}
	fmt.Println("Migrations completed.")
	return nil
}
