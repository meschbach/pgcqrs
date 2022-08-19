package restful

import (
	"context"
	"net/http"
)

func UnprocessableEntity(ctx context.Context, writer http.ResponseWriter, body string) {
	respondString(ctx, writer, 422, body)
}
