package main

import (
	"context"
	"encoding/json"
	"fmt"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/spf13/cobra"
	"os"
)

func configureStream(ctx context.Context, host, app, stream string) (*v1.Stream, error) {
	cfg := v1.Config{
		TransportType: v1.TransportTypeHTTP,
		ServiceURL:    host,
	}
	sys, err := cfg.SystemFromConfig()
	if err != nil {
		return nil, err
	}

	s, err := sys.Stream(ctx, app, stream)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func main() {
	var host string
	var app string
	var stream string
	var kind string

	queryAll := &cobra.Command{
		Use:   "all",
		Short: "retrieves all documents",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			stream, err := configureStream(cmd.Context(), host, app, stream)
			if err != nil {
				return err
			}

			envelopes, err := stream.All(ctx)
			if err != nil {
				return err
			}
			for _, e := range envelopes {
				var out json.RawMessage
				if err := stream.Get(ctx, e.ID, &out); err != nil {
					return err
				}
				fmt.Printf("%d(%s, %s): %s\n", e.ID, e.Kind, e.When, string(out))
			}
			return nil
		},
	}

	queryKind := &cobra.Command{
		Use:   "kind",
		Short: "retrieves documents related to a specific kind",
		RunE: func(cmd *cobra.Command, args []string) error {
			stream, err := configureStream(cmd.Context(), host, app, stream)
			if err != nil {
				return err
			}
			q := stream.Query()
			onKind := q.WithKind(kind)
			if len(args) > 0 {
				onKind = onKind.MatchDocument(args[0])
			}
			onKind.On(func(ctx context.Context, e v1.Envelope, rawJSON json.RawMessage) error {
				fmt.Printf("%d(%s, %s): %s\n", e.ID, e.Kind, e.When, string(rawJSON))
				return nil
			})
			if err := q.Stream(cmd.Context()); err != nil {
				return err
			}
			return nil
		},
	}

	query := &cobra.Command{
		Use:   "query",
		Short: "issues a query against an application and stream",
	}
	query.PersistentFlags().StringVarP(&app, "app", "a", os.Getenv("PGCQRS_APP"), "Application name to query")
	query.PersistentFlags().StringVarP(&stream, "stream", "s", os.Getenv("PGCQRS_STREAM"), "Stream to query")
	query.PersistentFlags().StringVarP(&kind, "kind", "k", "", "Kind to query")
	query.AddCommand(queryAll)
	query.AddCommand(queryKind)

	streamsList := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg := v1.Config{
				TransportType: v1.TransportTypeHTTP,
				ServiceURL:    host,
			}
			sys, err := cfg.SystemFromConfig()
			if err != nil {
				return err
			}

			pairs, err := sys.ListStreams(ctx)
			if err != nil {
				return err
			}
			for _, domain := range pairs {
				fmt.Printf("%#v\n", domain)
			}
			return nil
		},
	}
	streams := &cobra.Command{Use: "streams"}
	streams.AddCommand(streamsList)

	appsList := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Lists all apps or domains accessible within the host",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg := v1.Config{
				TransportType: v1.TransportTypeHTTP,
				ServiceURL:    host,
			}
			sys, err := cfg.SystemFromConfig()
			if err != nil {
				return err
			}

			domains, err := sys.ListDomains(ctx)
			if err != nil {
				return err
			}
			for _, domain := range domains {
				fmt.Printf("%s\n", domain)
			}
			return nil
		},
	}
	apps := &cobra.Command{
		Use:     "apps",
		Aliases: []string{"domains"},
		Short:   "Operations on apps or domains.",
	}
	apps.AddCommand(appsList)

	root := &cobra.Command{
		Use:           "pgcqrs",
		Short:         "CLI command line client",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&host, "host", "u", "http://localhost:9000", "Host to connect to")
	root.AddCommand(query)
	root.AddCommand(apps)
	root.AddCommand(streams)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Encoutnered error while servicing request: %s\n", err.Error())
		os.Exit(-1)
	}
}
