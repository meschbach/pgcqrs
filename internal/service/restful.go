package service

import (
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/meschbach/pgcqrs/internal/junk/restful"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"io/ioutil"
	"net/http"
	"time"
)

func (s *service) v1QueryRoute() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		var query v1.WireQuery
		if !restful.ParseRequestEntity(writer, request, &query) {
			return
		}

		var response v1.WireQueryResult
		response.Filtered = true
		response.SubsetMatch = true
		err := s.storage.applyQuery(ctx, app, stream, query, false, func(ctx context.Context, meta pgMeta, event json.RawMessage) error {
			response.Matching = append(response.Matching, presentMetaAsEnvelope(meta))
			return nil
		})
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		restful.Ok(writer, request, response)
	}
}

func (s *service) v1QueryBatchRoute() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		var query v1.WireQuery
		if !restful.ParseRequestEntity(writer, request, &query) {
			return
		}

		//TODO: modify query to retrieve data payload too
		var response v1.WireBatchResults
		err := s.storage.applyQuery(ctx, app, stream, query, true, func(ctx context.Context, meta pgMeta, data json.RawMessage) error {
			response.Page = append(response.Page, v1.WireBatchResultPair{
				Meta: presentMetaAsEnvelope(meta),
				Data: data,
			})
			return nil
		})
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		restful.Ok(writer, request, response)
	}
}

func (s *service) v1QueryBatchR2Route() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		var query v1.WireBatchR2Request
		if !restful.ParseRequestEntity(writer, request, &query) {
			return
		}

		operations := storage2.TranslateBatchR2(ctx, app, stream, &query)
		if len(operations) == 0 {
			restful.Ok(writer, request, v1.WireBatchR2Result{})
			return
		}
		eventsStream, runStream, err := s.repository.Stream(ctx, operations)
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		var streamError error
		go func() {
			_, streamError = runStream(ctx)
		}()

		out := v1.WireBatchR2Result{}
		for r := range eventsStream {
			out.Results = append(out.Results, v1.WireBatchR2Dispatch{
				Envelope: r.Envelope,
				Event:    r.Event,
				Op:       r.Op,
			})
		}
		if streamError != nil {
			restful.InternalError(writer, request, streamError)
			return
		}
		span := trace.SpanFromContext(ctx)
		span.AddEvent("events done")
		span.SetAttributes(attribute.Int("matches", len(out.Results)))
		restful.Ok(writer, request, out)
	}
}

func (s *service) v1QueryAllEnvelopes() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		//TODO: security
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		out := v1.AllEnvelopes{}
		err := s.storage.replayMeta(request.Context(), app, stream, func(ctx context.Context, meta pgMeta, entity json.RawMessage) error {
			out.Envelopes = append(out.Envelopes, presentMetaAsEnvelope(meta))
			return nil
		})
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}
		restful.Ok(writer, request, out)
	}
}

func (s *service) v1SubmitByKind() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]
		kind := vars["kind"]

		all, err := ioutil.ReadAll(request.Body)
		if err != nil {
			restful.ClientError(writer, request, err)
			return
		}

		id, err := s.storage.unsafeStore(ctx, app, stream, kind, all)
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}
		restful.Ok(writer, request, v1.SubmitReply{Id: id})

		s.bus.dispatchOnEventStored(ctx, app, stream, id, kind, all)
	}
}

func presentMetaAsEnvelope(meta pgMeta) v1.Envelope {
	return v1.Envelope{
		ID:   meta.ID,
		When: meta.When.Time.Format(time.RFC3339Nano),
		Kind: meta.Kind,
	}
}

func (s *service) v1Meta() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		var out v1.WireMetaV1
		rows, err := s.storage.query(ctx, "SELECT distinct app FROM events_stream")
		if err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		for rows.Next() {
			var app string
			if err := rows.Scan(&app); err != nil {
				restful.InternalError(writer, request, err)
				return
			}

			var streams []string
			queryStream := func() error {
				streamRows, err := s.storage.query(ctx, "SELECT stream FROM events_stream WHERE app = $1", app)
				if err != nil {
					return err
				}
				defer streamRows.Close()
				for streamRows.Next() {
					var stream string
					if err := streamRows.Scan(&stream); err != nil {
						return err
					}
					streams = append(streams, stream)
				}
				return nil
			}
			if err := queryStream(); err != nil {
				restful.InternalError(writer, request, err)
				return
			}
			out.Domains = append(out.Domains, v1.WireMetaDomainV1{
				Name:    app,
				Streams: streams,
			})
		}
		if err := rows.Err(); err != nil {
			restful.InternalError(writer, request, err)
			return
		}

		restful.Ok(writer, request, out)
	}
}
