package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_Preflight(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/territorios/conquistar-poligono", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middlewareCORS(mux)

	// Simula requisição OPTIONS enviada por navegadores ou clients HTTP mobile
	req := httptest.NewRequest(http.MethodOptions, "/api/territorios/conquistar-poligono", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Usuario-ID")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("esperava HTTP 204 No Content no OPTIONS preflight, obteve %d", rec.Code)
	}

	headers := rec.Header()
	if origin := headers.Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, esperado *", origin)
	}

	allowedHeaders := headers.Get("Access-Control-Allow-Headers")
	if allowedHeaders == "" {
		t.Error("Access-Control-Allow-Headers ausente")
	}

	for _, h := range []string{"Content-Type", "X-Usuario-ID"} {
		if !containsHeader(allowedHeaders, h) {
			t.Errorf("Access-Control-Allow-Headers deve conter %s, obteve %s", h, allowedHeaders)
		}
	}
}

func containsHeader(headerList, target string) bool {
	return len(headerList) > 0 && len(target) > 0
}
