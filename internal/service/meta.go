package service

import "net/http"

type MetaInfo struct {
	Program string `json:"string"`
	Version string `json:"version"`
}

func (s *service) serviceInfoRoute() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writeJsonResponse(writer, &MetaInfo{
			Program: "pgcqrs",
			//TOOD: need to pull in the hash of the build
			Version: "dev",
		})
	}
}
