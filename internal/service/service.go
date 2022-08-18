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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	v1Router.PathPrefix("/app/{app}/{stream}/all").Methods(http.MethodGet).HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		//TODO: security
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		out := v1.AllEnvelopes{}
		err := s.storage.replayMeta(request.Context(), app, stream, func(ctx context.Context, meta pgMeta) error {
			out.Envelopes = append(out.Envelopes, v1.Envelope{
				ID:   meta.ID,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: meta.Kind,
			})
			return nil
		})
		junk.Must(err)

		bytes, err := json.Marshal(out)
		junk.Must(err)

		_, err = writer.Write(bytes)
		junk.Must(err)
	})
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
	v1Router.Path("/app/{app}/{stream}/query").Methods(http.MethodPost).HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		requestEntity, err := ioutil.ReadAll(request.Body)
		junk.Must(err)

		var query v1.WireQuery
		if err := json.Unmarshal(requestEntity, &query); err != nil {
			writer.WriteHeader(422)
			writer.Write([]byte(err.Error()))
			return
		}

		ctx := request.Context()
		var response v1.WireQueryResult
		response.Filtered = false
		err = s.storage.applyQuery(ctx, app, stream, query, func(ctx context.Context, meta pgMeta) error {
			response.Matching = append(response.Matching, v1.Envelope{
				ID:   meta.ID,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: meta.Kind,
			})
			return nil
		})
		if err != nil {
			writer.WriteHeader(500)
			span := trace.SpanFromContext(ctx)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return
		}

		writer.WriteHeader(200)
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			span := trace.SpanFromContext(ctx)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	})
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
