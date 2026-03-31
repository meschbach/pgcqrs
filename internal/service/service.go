package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/meschbach/pgcqrs/internal/junk"
	"github.com/meschbach/pgcqrs/internal/junk/restful"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/thejerf/suture/v4"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
)

type service struct {
	storage    *storage
	repository *storage2.Repository
	bus        *bus
	positions  *storage2.PositionStore
}

// Result represents a generic operation result.
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
	v1Router.PathPrefix("/info").HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
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
		header := writer.Header()
		header.Set("Content-Type", "application/json")
		// We're writing JSON from the database directly to the wire; the only way we could make this better is to avoid
		// the copying from the database onto the wire.
		// nolint
		_, err = writer.Write(bytes)
		junk.Must(err)
	})
	v1Router.PathPrefix("/app/{app}/{stream}/submit/{kind}").Methods(http.MethodPost).HandlerFunc(s.v1SubmitByKind())
	v1Router.Path("/app/{app}/{stream}/query").Methods(http.MethodPost).HandlerFunc(s.v1QueryRoute())
	v1Router.Path("/app/{app}/{stream}/query-batch").Methods(http.MethodPost).HandlerFunc(s.v1QueryBatchRoute())
	v1Router.Path("/app/{app}/{stream}/query-batch-r2").Methods(http.MethodPost).HandlerFunc(s.v1QueryBatchR2Route())
	v1Router.Path("/app").Methods(http.MethodGet).HandlerFunc(s.v1Meta())

	v1Router.PathPrefix("/domains/{domain}/streams/{stream}/positions/{consumer}").Methods(http.MethodGet).HandlerFunc(s.getPosition())
	v1Router.PathPrefix("/domains/{domain}/streams/{stream}/positions/{consumer}").Methods(http.MethodPost).HandlerFunc(s.setPosition())
	v1Router.PathPrefix("/domains/{domain}/streams/{stream}/positions/{consumer}").Methods(http.MethodDelete).HandlerFunc(s.deletePosition())
	v1Router.PathPrefix("/domains/{domain}/streams/{stream}/positions").Methods(http.MethodGet).HandlerFunc(s.listConsumers())

	return root
}

func (s *service) getPosition() func(http.ResponseWriter, *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		vars := mux.Vars(request)
		domain := vars["domain"]
		stream := vars["stream"]
		consumer := vars["consumer"]

		eventID, found, err := s.positions.GetPosition(request.Context(), domain, stream, consumer)
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		type positionResponse struct {
			EventID int64 `json:"eventID"`
			Found   bool  `json:"found"`
		}
		bytes, err := json.Marshal(positionResponse{EventID: eventID, Found: found})
		junk.Must(err)
		writer.Header().Set("Content-Type", "application/json")
		_, err = writer.Write(bytes)
		junk.Must(err)
	}
}

func (s *service) setPosition() func(http.ResponseWriter, *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		vars := mux.Vars(request)
		domain := vars["domain"]
		stream := vars["stream"]
		consumer := vars["consumer"]

		type setPositionRequest struct {
			EventID int64 `json:"eventID"`
		}
		var req setPositionRequest
		if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		result, err := s.positions.SetPosition(request.Context(), domain, stream, consumer, req.EventID)
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		type setPositionResponse struct {
			Ok              bool   `json:"ok"`
			CurrentEventID  int64  `json:"currentEventID"`
			PreviousEventID *int64 `json:"previousEventID,omitempty"`
		}
		bytes, err := json.Marshal(setPositionResponse{Ok: true, CurrentEventID: result.CurrentEventID, PreviousEventID: result.PreviousEventID})
		junk.Must(err)
		writer.Header().Set("Content-Type", "application/json")
		_, err = writer.Write(bytes)
		junk.Must(err)
	}
}

func (s *service) deletePosition() func(http.ResponseWriter, *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		vars := mux.Vars(request)
		domain := vars["domain"]
		stream := vars["stream"]
		consumer := vars["consumer"]

		err := s.positions.DeletePosition(request.Context(), domain, stream, consumer)
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		bytes, err := json.Marshal(Result{Ok: true})
		junk.Must(err)
		writer.Header().Set("Content-Type", "application/json")
		_, err = writer.Write(bytes)
		junk.Must(err)
	}
}

func (s *service) listConsumers() func(http.ResponseWriter, *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		vars := mux.Vars(request)
		domain := vars["domain"]
		stream := vars["stream"]

		consumers, err := s.positions.ListConsumers(request.Context(), domain, stream)
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		type listConsumersResponse struct {
			Consumers []string `json:"consumers"`
		}
		bytes, err := json.Marshal(listConsumersResponse{Consumers: consumers})
		junk.Must(err)
		writer.Header().Set("Content-Type", "application/json")
		_, err = writer.Write(bytes)
		junk.Must(err)
	}
}

func (s *service) serve(_ context.Context, config *ListenerConfig) {
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

// Serve starts the CQRS service with the given configuration.
func Serve(ctx context.Context, cfg *Config) {
	fmt.Println("Starting PG-CQRS Service")
	component, err := cfg.Telemetry.Start(ctx)
	if err != nil {
		panic(err)
	}
	//nolint
	go func() {
		<-ctx.Done()
		//nolint
		shutdownCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := component.ShutdownGracefully(shutdownCtx); err != nil {
			panic(err)
		}
	}()

	var appDone <-chan error
	s := &service{
		bus: newBus(),
	}

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
		s.positions = storage2.NewPositionStore(pool)

		app := suture.NewSimple("pgcqrs")
		if cfg.GRPCListener != nil {
			app.Add(&grpcPort{
				config:    cfg.GRPCListener,
				oldCore:   s.storage,
				core:      s.repository,
				bus:       s.bus,
				positions: storage2.NewPositionStore(pool),
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
