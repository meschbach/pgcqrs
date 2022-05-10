package migrator

import (
	"context"
	"fmt"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func MigratePrimary(ctx context.Context, config Config) error {
	dir := "file://" + config.MigrationDir
	db := "pgx://" + config.Storage.Primary.DatabaseURL
	fmt.Printf("Migration from %q (%q)\n", dir, db)

	migrator, err := migrate.New(dir, db)
	if err != nil {
		fmt.Println("Migration creation failed")
		return err
	}
	defer migrator.Close()
	migrator.Log = &migratorLogger{}
	if err := migrator.Up(); err != nil {
		if err.Error() != "no change" {
			return err
		}
	}
	fmt.Println("Migrations completed.")
	return nil
}
