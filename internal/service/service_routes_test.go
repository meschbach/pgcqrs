package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDataAndOpsRoutesSplit(t *testing.T) {
	t.Parallel()

	s := &service{}

	dataHandler := s.dataRoutes()
	opsHandler := s.opsRoutes()

	reqOpsLive := httptest.NewRequest(http.MethodGet, "/ops/liveness", http.NoBody)
	wData := httptest.NewRecorder()
	dataHandler.ServeHTTP(wData, reqOpsLive)
	if wData.Code != http.StatusNotFound {
		t.Errorf("dataRoutes /ops/liveness expected 404, got %d", wData.Code)
	}

	reqOpsReady := httptest.NewRequest(http.MethodGet, "/ops/readiness", http.NoBody)
	wOps := httptest.NewRecorder()
	opsHandler.ServeHTTP(wOps, reqOpsReady)
	if wOps.Code != http.StatusOK {
		t.Errorf("opsRoutes /ops/readiness expected 200, got %d", wOps.Code)
	}

	reqOpsRoot := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	wOpsRoot := httptest.NewRecorder()
	opsHandler.ServeHTTP(wOpsRoot, reqOpsRoot)
	if wOpsRoot.Code != http.StatusOK {
		t.Errorf("opsRoutes / expected 200, got %d", wOpsRoot.Code)
	}
	body := wOpsRoot.Body.String()
	if body == "" {
		t.Errorf("opsRoutes / expected HTML body")
	}
}
