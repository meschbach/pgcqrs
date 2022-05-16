package service

import (
	"encoding/json"
	"net/http"
)

type HealCheckReply struct {
	Ok bool `json:"ok"`
}

func writeJsonResponse(writer http.ResponseWriter, entity interface{}) {
	encoder := json.NewEncoder(writer)
	if err := encoder.Encode(entity); err != nil {
		panic(err)
	}
}

func (s *service) livenessRoute() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writeJsonResponse(writer, &HealCheckReply{
			Ok: true,
		})
	}
}

func (s *service) readinessRoute() http.HandlerFunc {
	//TOOD: should have a better check for being ready
	return func(writer http.ResponseWriter, request *http.Request) {
		writeJsonResponse(writer, &HealCheckReply{
			Ok: true,
		})
	}
}
