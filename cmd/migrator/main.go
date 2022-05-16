package main

import (
	"fmt"
	"github.com/meschbach/go-junk-bucket/pkg/files"
	"github.com/meschbach/pgcqrs/internal"
	"github.com/meschbach/pgcqrs/internal/migrator"
	"github.com/spf13/cobra"
	"os"
)

func main() {
	migrationsDir := "migrations"
	storageCredentials := "secrets/primary.json"

	serve := cobra.Command{
		Use:   "primary",
		Short: "Migrates teh primary repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := migrator.Config{
				Storage:      internal.Storage{},
				MigrationDir: migrationsDir + "/primary",
			}
			if err := files.ParseJSONFile(storageCredentials, &cfg); err != nil {
				return err
			}
			return migrator.MigratePrimary(cmd.Context(), cfg)
		},
	}

	root := cobra.Command{
		Use:           "migrator",
		Short:         "Migrations against pgcqrs system.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&migrationsDir, "migrations-dir", "m", migrationsDir, "Base directory fo stored migrations")
	root.AddCommand(&serve)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Encoutnered error while servicing request: %s\n", err.Error())
		os.Exit(-1)
	}
}
