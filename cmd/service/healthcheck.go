package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func healthCheckCommand() *cobra.Command {
	opsCheck := &cobra.Command{
		Use:   "ops",
		Short: "Checks the readiness of the ops listener.",
		RunE: func(cmd *cobra.Command, args []string) error {
			address := os.Getenv("PGCQRS_OPS_LISTENER_ADDRESS")
			if address == "" {
				address = "127.0.0.1:9002"
			}
			if strings.HasPrefix(address, "0.0.0.0:") {
				address = "127.0.0.1:" + address[len("0.0.0.0:"):]
			}
			timedContext, done := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer done()

			client := &http.Client{Timeout: 5 * time.Second}
			url := "http://" + address + "/ops/readiness"
			req, err := http.NewRequestWithContext(timedContext, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Ops readiness check failed: %v\n", err)
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Println("Ops readiness OK.")
				return nil
			}
			fmt.Printf("Ops readiness returned %d.\n", resp.StatusCode)
			return fmt.Errorf("ops readiness returned %d", resp.StatusCode)
		},
	}

	healthCheck := &cobra.Command{
		Use:   "health-check",
		Short: "Tools to check health of the service.",
	}
	healthCheck.AddCommand(opsCheck)

	return healthCheck
}
