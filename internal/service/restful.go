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
		err = s.storage.applyQuery(ctx, app, stream, query, func(ctx context.Context, meta pgMeta) error {
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
