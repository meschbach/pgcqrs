package service

import (
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/meschbach/pgcqrs/internal/junk"
	"github.com/meschbach/pgcqrs/internal/junk/restful"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
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

		requestEntity, err := ioutil.ReadAll(request.Body)
		junk.Must(err)

		var query v1.WireQuery
		if err := json.Unmarshal(requestEntity, &query); err != nil {
			restful.UnprocessableEntity(ctx, writer, err.Error())
			return
		}

		var response v1.WireQueryResult
		response.Filtered = true
		response.SubsetMatch = true
		err = s.storage.applyQuery(ctx, app, stream, query, false, func(ctx context.Context, meta pgMeta, event json.RawMessage) error {
			response.Matching = append(response.Matching, v1.Envelope{
				ID:   meta.ID,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: meta.Kind,
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

func (s *service) v1QueryBatchRoute() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		requestEntity, err := ioutil.ReadAll(request.Body)
		junk.Must(err)

		var query v1.WireQuery
		if err := json.Unmarshal(requestEntity, &query); err != nil {
			restful.UnprocessableEntity(ctx, writer, err.Error())
			return
		}

		//TODO: modify query to retrieve data payload too
		var response v1.WireBatchResults
		err = s.storage.applyQuery(ctx, app, stream, query, true, func(ctx context.Context, meta pgMeta, data json.RawMessage) error {
			e := v1.Envelope{
				ID:   meta.ID,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: meta.Kind,
			}

			response.Page = append(response.Page, v1.WireBatchResultPair{
				Meta: e,
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

func (s *service) v1QueryAllEnvelopes() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		//TODO: security
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]

		out := v1.AllEnvelopes{}
		err := s.storage.replayMeta(request.Context(), app, stream, func(ctx context.Context, meta pgMeta, entity json.RawMessage) error {
			out.Envelopes = append(out.Envelopes, v1.Envelope{
				ID:   meta.ID,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: meta.Kind,
			})
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
		vars := mux.Vars(request)
		app := vars["app"]
		stream := vars["stream"]
		kind := vars["kind"]

		all, err := ioutil.ReadAll(request.Body)
		if err != nil {
			restful.ClientError(writer, request, err)
			return
		}

		//TODO: Should properly chain errors.
		id := s.storage.store(request.Context(), app, stream, kind, all)
		restful.Ok(writer, request, v1.SubmitReply{Id: id})
	}
}
