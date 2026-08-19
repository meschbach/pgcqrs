package main

import (
	"fmt"
	"os"

	"github.com/meschbach/go-junk-bucket/pkg"
	"github.com/meschbach/go-junk-bucket/pkg/files"
	"github.com/meschbach/pgcqrs/internal"
	"github.com/meschbach/pgcqrs/internal/migrator"
	"github.com/spf13/cobra"
)

func main() {
	primaryStorageFile := pkg.EnvOrDefault("CFG_PRIMARY", "")

	serve := cobra.Command{
		Use:   "primary",
		Short: "Migrates teh primary repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := migrator.Config{
				Storage: internal.Storage{},
			}
			if primaryStorageFile != "" {
				if err := files.ParseJSONFile(primaryStorageFile, &cfg); err != nil {
					return err
				}
			}
			if value := pkg.EnvOrDefault("PGCQRS_STORAGE_POSTGRES_URL", ""); value != "" {
				cfg.Storage.Primary.DatabaseURL = value
			}
			return migrator.MigratePrimary(cmd.Context(), cfg)
		},
	}

	down := cobra.Command{
		Use:   "down",
		Short: "Rolls back one migration from the primary repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := migrator.Config{
				Storage: internal.Storage{},
			}
			if primaryStorageFile != "" {
				if err := files.ParseJSONFile(primaryStorageFile, &cfg); err != nil {
					return err
				}
			}
			if value := pkg.EnvOrDefault("PGCQRS_STORAGE_POSTGRES_URL", ""); value != "" {
				cfg.Storage.Primary.DatabaseURL = value
			}
			return migrator.MigratePrimaryDown(cmd.Context(), cfg)
		},
	}

	version := cobra.Command{
		Use:   "version",
		Short: "Reports the current migration version of the primary repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := migrator.Config{
				Storage: internal.Storage{},
			}
			if primaryStorageFile != "" {
				if err := files.ParseJSONFile(primaryStorageFile, &cfg); err != nil {
					return err
				}
			}
			if value := pkg.EnvOrDefault("PGCQRS_STORAGE_POSTGRES_URL", ""); value != "" {
				cfg.Storage.Primary.DatabaseURL = value
			}
			return migrator.MigratePrimaryVersion(cmd.Context(), cfg)
		},
	}

	root := cobra.Command{
		Use:           "migrator",
		Short:         "Migrations against pgcqrs system.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(&serve)
	root.AddCommand(&down)
	root.AddCommand(&version)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Encoutnered error while servicing request: %s\n", err.Error())
		os.Exit(-1)
	}
}
