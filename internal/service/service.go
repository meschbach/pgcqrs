package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/meschbach/go-junk-bucket/pkg/observability"
	"github.com/meschbach/pgcqrs/internal/junk"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"
)

type service struct {
	storage *storage
}

type Result struct {
	Ok bool `json:"ok"`
}

func (s *service) routes() http.Handler {
	root := mux.NewRouter()
	root.HandleFunc("/", s.serviceInfoRoute())

	ops := root.PathPrefix("/ops").Subrouter()
	ops.HandleFunc("/liveness", s.livenessRoute())
	ops.HandleFunc("/readiness", s.readinessRoute())

	v1Router := root.PathPrefix("/v1").Subrouter()
	v1Router.Use(otelmux.Middleware("pgcqrs.http.v1"))
	v1Router.PathPrefix("/info").HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		bytes, err := json.Marshal("pg-cqrs")
		junk.Must(err)
		_, err = writer.Write(bytes)
		junk.Must(err)
	})
	v1Router.PathPrefix("/app/{app}/{stream}").Methods(http.MethodPut).HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		//TODO: security
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		junk.Must(s.storage.ensureStream(request.Context(), app, stream))

		bytes, err := json.Marshal(Result{Ok: true})
		junk.Must(err)

		_, err = writer.Write(bytes)
		junk.Must(err)
	})
	v1Router.PathPrefix("/app/{app}/{stream}/all").Methods(http.MethodGet).HandlerFunc(s.v1QueryAllEnvelopes())
	v1Router.PathPrefix("/app/{app}/{stream}/payload/{id}").Methods(http.MethodGet).HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		//TODO: security
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]
		idString := vars["id"]

		id, err := strconv.ParseInt(idString, 10, 64)
		if err != nil {
			writer.WriteHeader(404)
			return
		}

		bytes, err := s.storage.fetchPayload(request.Context(), app, stream, id)
		junk.Must(err)
		_, err = writer.Write(bytes)
		junk.Must(err)
	})
	v1Router.PathPrefix("/app/{app}/{stream}/submit/{kind}").Methods(http.MethodPost).HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]
		kind := vars["kind"]

		all, err := ioutil.ReadAll(request.Body)
		junk.Must(err)

		id := s.storage.store(request.Context(), app, stream, kind, all)
		bytes, err := json.Marshal(v1.SubmitReply{Id: id})
		junk.Must(err)

		_, err = writer.Write(bytes)
		junk.Must(err)
	})
	v1Router.Path("/app/{app}/{stream}/query").Methods(http.MethodPost).HandlerFunc(s.v1QueryRoute())
	v1Router.Path("/app/{app}/{stream}/query-batch").Methods(http.MethodPost).HandlerFunc(s.v1QueryBatchRoute())
	return root
}

func (s *service) serve(ctx context.Context, config *ListenerConfig) {
	listenerAddress := "localhost:9000"
	if config != nil {
		listenerAddress = config.Address
	}
	server := &http.Server{
		Handler:      s.routes(),
		Addr:         listenerAddress,
		WriteTimeout: 30 * time.Second,
		ReadTimeout:  30 * time.Second,
	}
	fmt.Printf("Serving traffic at %s\n", listenerAddress)
	var err error
	if config != nil && config.TLS != nil {
		err = server.ListenAndServeTLS(*config.TLS.CertificateFile, *config.TLS.KeyFile)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil {
		panic(err)
	}
}

func Serve(ctx context.Context, cfg Config) {
	fmt.Println("Starting PG-CQRS Service")
	if err := observability.SetupTracing(ctx, cfg.Telemetry); err != nil {
		panic(err)
	}

	s := &service{}
	func() {
		startup, span := tracer.Start(ctx, "pgcqrs.start")
		defer span.End()
		pool, err := pgxpool.Connect(startup, "postgres://"+cfg.Storage.Primary.DatabaseURL)
		if err != nil {
			panic(err)
		}
		s.storage = &storage{pg: pool}
		s.serve(startup, cfg.Listener)
	}()
}
