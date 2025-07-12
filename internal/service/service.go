package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/meschbach/pgcqrs/internal/junk"
	"github.com/meschbach/pgcqrs/internal/junk/restful"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/thejerf/suture/v4"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"net/http"
	"os"
	"strconv"
	"time"
)

type service struct {
	storage    *storage
	repository *storage2.Repository
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

		if err := s.storage.ensureStream(request.Context(), app, stream); err != nil {
			restful.InternalError(writer, request, err)
			return
		}

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
	v1Router.PathPrefix("/app/{app}/{stream}/submit/{kind}").Methods(http.MethodPost).HandlerFunc(s.v1SubmitByKind())
	v1Router.Path("/app/{app}/{stream}/query").Methods(http.MethodPost).HandlerFunc(s.v1QueryRoute())
	v1Router.Path("/app/{app}/{stream}/query-batch").Methods(http.MethodPost).HandlerFunc(s.v1QueryBatchRoute())
	v1Router.Path("/app/{app}/{stream}/query-batch-r2").Methods(http.MethodPost).HandlerFunc(s.v1QueryBatchR2Route())
	v1Router.Path("/app").Methods(http.MethodGet).HandlerFunc(s.v1Meta())
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
	component, err := cfg.Telemetry.Start(ctx)
	if err != nil {
		panic(err)
	}
	go func() {
		<-ctx.Done()
		ctx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := component.ShutdownGracefully(ctx); err != nil {
			panic(err)
		}
	}()

	var appDone <-chan error
	s := &service{}
	func() {
		startup, span := tracer.Start(ctx, "pgcqrs.start")
		defer span.End()
		pool, err := pgxpool.New(startup, "postgres://"+cfg.Storage.Primary.DatabaseURL)
		if err != nil {
			panic(err)
		}
		connConfig := pool.Config().ConnConfig
		fmt.Printf("Connected to database: user=%s host=%s database=%s\n", connConfig.User, connConfig.Host, connConfig.Database)
		s.storage = &storage{pg: pool}
		s.repository = storage2.RepositoryWithPool(pool)

		app := suture.NewSimple("pgcqrs")
		if cfg.GRPCListener != nil {
			app.Add(&grpcPort{
				config:  cfg.GRPCListener,
				oldCore: s.storage,
				core:    s.repository,
			})
		}
		appDone = app.ServeBackground(ctx)
		s.serve(startup, cfg.Listener)
	}()
	appDoneError := <-appDone
	if appDoneError != nil {
		fmt.Fprintf(os.Stderr, "Error with app: %s\n", appDoneError.Error())
	}
}
