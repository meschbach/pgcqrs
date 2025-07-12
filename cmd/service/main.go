package main

import (
	"fmt"
	"github.com/meschbach/go-junk-bucket/pkg"
	"github.com/meschbach/go-junk-bucket/pkg/files"
	"github.com/meschbach/pgcqrs/internal/service"
	"github.com/spf13/cobra"
	"os"
)

func main() {
	primaryStorageFile := pkg.EnvOrDefault("CFG_PRIMARY", "")

	serve := cobra.Command{
		Use:   "serve",
		Short: "starts service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := service.Config{}
			cfg.LoadDefaults()
			if primaryStorageFile != "" {
				err := files.ParseJSONFile(primaryStorageFile, &cfg)
				if err != nil {
					return err
				}
			}
			cmdContext := cmd.Context()
			service.Serve(cmdContext, cfg)
			return nil
		},
	}

	root := cobra.Command{
		Use:           "service",
		Short:         "Subscriptions service commands",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(&serve)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Encoutnered error while servicing request: %s\n", err.Error())
		os.Exit(-1)
	}
}
