package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicHandlerServesOnlyTheAllowedRoutes(t *testing.T) {
	_, registry, _, _ := newAuthServer(t, false)
	public := httptest.NewServer((&Server{
		Registry: registry,
		AdminKey: testAdminKey,
		Version:  "test",
	}).PublicHandler())
	defer public.Close()

	for _, path := range []string{"/v1/ping", "/v1/version"} {
		response, err := http.Get(public.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Errorf("%s must stay public: status = %d", path, response.StatusCode)
		}
	}

	closed := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/peers"},
		{http.MethodGet, "/v1/devices"},
		{http.MethodPost, "/v1/presence"},
		{http.MethodPost, "/v1/signal"},
		{http.MethodGet, "/v1/tokens"},
		{http.MethodGet, "/v1/invites"},
		{http.MethodPost, "/v1/claim"},
	}
	for _, item := range closed {
		request, err := http.NewRequest(item.method, public.URL+item.path, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		request.Header.Set("Authorization", "Bearer "+testAdminKey)
		status, payload := send(t, request)
		if status != http.StatusNotFound {
			t.Errorf("%s %s must be closed to the world: status = %d, payload = %v", item.method, item.path, status, payload)
		}
		if payload["reason"] != "not_public" {
			t.Errorf("%s %s: reason = %v", item.method, item.path, payload["reason"])
		}
	}

	request, err := http.NewRequest(http.MethodPost, public.URL+"/v1/enroll", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	status, _ := send(t, request)
	if status == http.StatusNotFound {
		t.Error("enrollment must stay reachable on the public listener")
	}
}
