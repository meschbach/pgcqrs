package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouteIsolation(t *testing.T) {
	t.Parallel()

	s := &service{}
	dataHandler := s.dataRoutes()
	opsHandler := s.opsRoutes()

	// Client should return 404 for /ops/*
	w := httptest.NewRecorder()
	dataHandler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ops/liveness", http.NoBody))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /ops/liveness on dataRoutes, got %d", w.Code)
	}

	// Ops should return 404 for /v1/*
	w2 := httptest.NewRecorder()
	opsHandler.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/v1/info", http.NoBody))
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /v1/info on opsRoutes, got %d", w2.Code)
	}
}
