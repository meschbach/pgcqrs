package service

import "net/http"

// MetaInfo represents metadata about the CQRS service.
type MetaInfo struct {
	Program string `json:"string"`
	Version string `json:"version"`
}

func (s *service) serviceInfoRoute() http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(writer, &MetaInfo{
			Program: "pgcqrs",
			//TOOD: need to pull in the hash of the build
			Version: "dev",
		})
	}
}
