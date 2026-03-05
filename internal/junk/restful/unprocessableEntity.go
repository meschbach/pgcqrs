package restful

import (
	"context"
	"net/http"
)

// UnprocessableEntity writes an unprocessable entity (422) response.
func UnprocessableEntity(ctx context.Context, writer http.ResponseWriter, body string) {
	respondString(ctx, writer, 422, body)
}
