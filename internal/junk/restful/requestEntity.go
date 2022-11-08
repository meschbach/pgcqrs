package restful

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
)

// ParseRequestEntity attempts to parse the request entity into the target entity.  True if successful, otherwise an
// error is written to the response and false is returned.
func ParseRequestEntity[E any](writer http.ResponseWriter, request *http.Request, entity *E) bool {
	requestEntity, err := ioutil.ReadAll(request.Body)
	if err != nil {
		ClientError(writer, request, err)
		return false
	}

	if err := json.Unmarshal(requestEntity, entity); err != nil {
		UnprocessableEntity(request.Context(), writer, err.Error())
		return false
	}
	return true
}
