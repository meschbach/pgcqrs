// Package migrator handles database migrations for the CQRS service.
package migrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	// Register database driver for migrations.
	_ "github.com/golang-migrate/migrate/v4/database/pgx"
	// Register source driver for migrations.
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/meschbach/pgcqrs/migrations"
)

// MigratePrimary runs the primary database migrations.
func MigratePrimary(_ context.Context, config Config) (problem error) {
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

// MigratePrimaryVersion reports the current migration version of the primary database.
func MigratePrimaryVersion(_ context.Context, config Config) (problem error) {
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
	version, dirty, err := migrator.Version()
	if err != nil {
		if err.Error() == "no change" {
			fmt.Println("Current version: 0 (no migrations applied)")
			return nil
		}
		return err
	}
	if dirty {
		fmt.Printf("Current version: %d (dirty)\n", version)
	} else {
		fmt.Printf("Current version: %d\n", version)
	}
	return nil
}

// MigratePrimaryDown rolls back one migration from the primary database.
func MigratePrimaryDown(_ context.Context, config Config) (problem error) {
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
	if err := migrator.Steps(-1); err != nil {
		if err.Error() != "no change" {
			return err
		}
	}
	fmt.Println("Migration rollback completed.")
	return nil
}
