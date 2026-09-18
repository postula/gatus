package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TwiN/gatus/v5/config"
	"github.com/TwiN/gatus/v5/config/endpoint"
	"github.com/TwiN/gatus/v5/config/endpoint/ui"
	configui "github.com/TwiN/gatus/v5/config/ui"
	"github.com/TwiN/gatus/v5/config/visibility"
	"github.com/TwiN/gatus/v5/security"
	"github.com/TwiN/gatus/v5/storage/store"
	"github.com/TwiN/gatus/v5/watchdog"
)

func TestRequireBadgeVisibility(t *testing.T) {
	defer store.Get().Clear()
	defer cache.Clear()
	endpoints := []*endpoint.Endpoint{
		{Name: "private", Group: "core"},
		{Name: "public", Group: "core", Visibility: visibility.Visibility{Public: true}},
		{Name: "badges", Group: "core", Visibility: visibility.Visibility{Badges: true}},
	}
	for _, ep := range endpoints {
		ep.UIConfig = ui.GetDefaultConfig()
		watchdog.UpdateEndpointStatus(ep, &endpoint.Result{Success: true, Connected: true, Duration: time.Millisecond, Timestamp: time.Now(), Public: ep.Visibility.Public})
	}
	basic := &security.Config{Basic: &security.BasicConfig{
		Username:                        "john.doe",
		PasswordBcryptHashBase64Encoded: "JDJhJDA4JDFoRnpPY1hnaFl1OC9ISlFsa21VS09wOGlPU1ZOTDlHZG1qeTFvb3dIckRBUnlHUmNIRWlT",
	}}
	scenarios := []struct {
		Name          string
		Security      *security.Config
		Key           string
		Authenticated bool
		ExpectedCode  int
	}{
		{Name: "no-security-private", Key: "core_private", ExpectedCode: 200},
		{Name: "anonymous-private", Security: basic, Key: "core_private", ExpectedCode: 404},
		{Name: "anonymous-public", Security: basic, Key: "core_public", ExpectedCode: 200},
		{Name: "anonymous-badges", Security: basic, Key: "core_badges", ExpectedCode: 200},
		{Name: "anonymous-unknown", Security: basic, Key: "core_unknown", ExpectedCode: 404},
		{Name: "authenticated-private", Security: basic, Key: "core_private", Authenticated: true, ExpectedCode: 200},
	}
	paths := []string{
		"/api/v1/endpoints/%s/health/badge.svg",
		"/api/v1/endpoints/%s/health/badge.shields",
		"/api/v1/endpoints/%s/uptimes/1h",
		"/api/v1/endpoints/%s/uptimes/1h/badge.svg",
		"/api/v1/endpoints/%s/response-times/1h",
		"/api/v1/endpoints/%s/response-times/1h/badge.svg",
		"/api/v1/endpoints/%s/response-times/24h/chart.svg",
		"/api/v1/endpoints/%s/response-times/24h/history",
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			router := New(&config.Config{Endpoints: endpoints, Security: scenario.Security, UI: &configui.Config{}}).Router()
			for _, path := range paths {
				request := httptest.NewRequest("GET", fmt.Sprintf(path, scenario.Key), http.NoBody)
				if scenario.Authenticated {
					request.SetBasicAuth("john.doe", "hunter2")
				}
				response, err := router.Test(request)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != scenario.ExpectedCode {
					t.Errorf("%s: expected %d, got %d", request.URL.Path, scenario.ExpectedCode, response.StatusCode)
				}
			}
		})
	}
}
