package main

import (
	"context"
	"fmt"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/spf13/cobra"
	"time"
)

func healthCheckCommand() *cobra.Command {
	httpCheck := &cobra.Command{
		Use:   "http",
		Short: "Checks the health of the service via HTTP.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("HTTP based health check.")
			timedContext, done := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer done()

			t := v1.NewHttpTransport("http://localhost:9000")
			data, err := t.Meta(timedContext)
			fmt.Printf("Successful with %d domains.\n", len(data.Domains))
			return err
		},
	}

	healthCheck := &cobra.Command{
		Use:   "health-check",
		Short: "Tools to check health of the service.",
	}
	healthCheck.AddCommand(httpCheck)

	return healthCheck
}
