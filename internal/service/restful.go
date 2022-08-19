package service

import (
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/meschbach/pgcqrs/internal/junk"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"io/ioutil"
	"net/http"
	"time"
)

func (s *service) v1QueryRoute() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
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
	}
}
