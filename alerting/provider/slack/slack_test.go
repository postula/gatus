package slack

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/TwiN/gatus/v5/alerting/alert"
	"github.com/TwiN/gatus/v5/client"
	"github.com/TwiN/gatus/v5/config/endpoint"
	"github.com/TwiN/gatus/v5/test"
)

func TestAlertProvider_Validate(t *testing.T) {
	invalidProvider := AlertProvider{DefaultConfig: Config{WebhookURL: ""}}
	if err := invalidProvider.Validate(); err == nil {
		t.Error("provider shouldn't have been valid")
	}
	validProvider := AlertProvider{DefaultConfig: Config{WebhookURL: "https://example.com"}}
	if err := validProvider.Validate(); err != nil {
		t.Error("provider should've been valid")
	}
	botProviderWithoutChannel := AlertProvider{DefaultConfig: Config{BotToken: "xoxb-token"}}
	if err := botProviderWithoutChannel.Validate(); err == nil {
		t.Error("provider without channel shouldn't have been valid")
	}
	validBotProvider := AlertProvider{DefaultConfig: Config{BotToken: "xoxb-token", Channel: "#alerts"}}
	if err := validBotProvider.Validate(); err != nil {
		t.Error("bot provider should've been valid")
	}
}

func TestAlertProvider_ValidateWithOverride(t *testing.T) {
	providerWithInvalidOverrideGroup := AlertProvider{
		Overrides: []Override{
			{
				Config: Config{WebhookURL: "http://example.com"},
				Group:  "",
			},
		},
	}
	if err := providerWithInvalidOverrideGroup.Validate(); err == nil {
		t.Error("provider Group shouldn't have been valid")
	}
	providerWithInvalidOverrideTo := AlertProvider{
		Overrides: []Override{
			{
				Config: Config{WebhookURL: ""},
				Group:  "group",
			},
		},
	}
	if err := providerWithInvalidOverrideTo.Validate(); err == nil {
		t.Error("provider integration key shouldn't have been valid")
	}
	providerWithValidOverride := AlertProvider{
		DefaultConfig: Config{WebhookURL: "http://example.com"},
		Overrides: []Override{
			{
				Config: Config{WebhookURL: "http://example.com"},
				Group:  "group",
			},
		},
	}
	if err := providerWithValidOverride.Validate(); err != nil {
		t.Error("provider should've been valid")
	}
}

func TestAlertProvider_Send(t *testing.T) {
	defer client.InjectHTTPClient(nil)
	firstDescription := "description-1"
	secondDescription := "description-2"
	scenarios := []struct {
		Name             string
		Provider         AlertProvider
		Alert            alert.Alert
		Resolved         bool
		MockRoundTripper test.MockRoundTripper
		ExpectedError    bool
	}{
		{
			Name:     "triggered",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: false,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
			}),
			ExpectedError: false,
		},
		{
			Name:     "triggered-error",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: false,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}
			}),
			ExpectedError: true,
		},
		{
			Name:     "resolved",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: true,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}
			}),
			ExpectedError: false,
		},
		{
			Name:     "resolved-error",
			Provider: AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Alert:    alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved: true,
			MockRoundTripper: test.MockRoundTripper(func(r *http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}
			}),
			ExpectedError: true,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			client.InjectHTTPClient(&http.Client{Transport: scenario.MockRoundTripper})
			err := scenario.Provider.Send(
				&endpoint.Endpoint{Name: "endpoint-name"},
				&scenario.Alert,
				&endpoint.Result{
					ConditionResults: []*endpoint.ConditionResult{
						{Condition: "[CONNECTED] == true", Success: scenario.Resolved},
						{Condition: "[STATUS] == 200", Success: scenario.Resolved},
					},
				},
				scenario.Resolved,
			)
			if scenario.ExpectedError && err == nil {
				t.Error("expected error, got none")
			}
			if !scenario.ExpectedError && err != nil {
				t.Error("expected no error, got", err.Error())
			}
		})
	}
}

func TestAlertProvider_buildRequestBody(t *testing.T) {
	firstDescription := "description-1"
	secondDescription := "description-2"
	scenarios := []struct {
		Name         string
		Provider     AlertProvider
		Endpoint     endpoint.Endpoint
		Alert        alert.Alert
		NoConditions bool
		Resolved     bool
		ExpectedBody string
	}{
		{
			Name:         "triggered",
			Provider:     AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Endpoint:     endpoint.Endpoint{Name: "name"},
			Alert:        alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     false,
			ExpectedBody: "{\"text\":\"\",\"attachments\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"text\":\"An alert for *name* has been triggered due to having failed 3 time(s) in a row:\\n\\u003e description-1\",\"short\":false,\"color\":\"#DD0000\",\"fields\":[{\"title\":\"Condition results\",\"value\":\":x: - `[CONNECTED] == true`\\n:x: - `[STATUS] == 200`\\n\",\"short\":false}]}]}",
		},
		{
			Name:         "triggered-with-group",
			Provider:     AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Endpoint:     endpoint.Endpoint{Name: "name", Group: "group"},
			Alert:        alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     false,
			ExpectedBody: "{\"text\":\"\",\"attachments\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"text\":\"An alert for *group/name* has been triggered due to having failed 3 time(s) in a row:\\n\\u003e description-1\",\"short\":false,\"color\":\"#DD0000\",\"fields\":[{\"title\":\"Condition results\",\"value\":\":x: - `[CONNECTED] == true`\\n:x: - `[STATUS] == 200`\\n\",\"short\":false}]}]}",
		},
		{
			Name:         "triggered-with-no-conditions",
			NoConditions: true,
			Provider:     AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Endpoint:     endpoint.Endpoint{Name: "name"},
			Alert:        alert.Alert{Description: &firstDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     false,
			ExpectedBody: "{\"text\":\"\",\"attachments\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"text\":\"An alert for *name* has been triggered due to having failed 3 time(s) in a row:\\n\\u003e description-1\",\"short\":false,\"color\":\"#DD0000\"}]}",
		},
		{
			Name:         "resolved",
			Provider:     AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Endpoint:     endpoint.Endpoint{Name: "name"},
			Alert:        alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     true,
			ExpectedBody: "{\"text\":\"\",\"attachments\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"text\":\"An alert for *name* has been resolved after passing successfully 5 time(s) in a row:\\n\\u003e description-2\",\"short\":false,\"color\":\"#36A64F\",\"fields\":[{\"title\":\"Condition results\",\"value\":\":white_check_mark: - `[CONNECTED] == true`\\n:white_check_mark: - `[STATUS] == 200`\\n\",\"short\":false}]}]}",
		},
		{
			Name:         "resolved-with-group",
			Provider:     AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com"}},
			Endpoint:     endpoint.Endpoint{Name: "name", Group: "group"},
			Alert:        alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     true,
			ExpectedBody: "{\"text\":\"\",\"attachments\":[{\"title\":\":helmet_with_white_cross: Gatus\",\"text\":\"An alert for *group/name* has been resolved after passing successfully 5 time(s) in a row:\\n\\u003e description-2\",\"short\":false,\"color\":\"#36A64F\",\"fields\":[{\"title\":\"Condition results\",\"value\":\":white_check_mark: - `[CONNECTED] == true`\\n:white_check_mark: - `[STATUS] == 200`\\n\",\"short\":false}]}]}",
		},
		{
			Name:         "resolved-with-group-and-custom-title",
			Provider:     AlertProvider{DefaultConfig: Config{WebhookURL: "http://example.com", Title: "custom title"}},
			Endpoint:     endpoint.Endpoint{Name: "name", Group: "group"},
			Alert:        alert.Alert{Description: &secondDescription, SuccessThreshold: 5, FailureThreshold: 3},
			Resolved:     true,
			ExpectedBody: "{\"text\":\"\",\"attachments\":[{\"title\":\"custom title\",\"text\":\"An alert for *group/name* has been resolved after passing successfully 5 time(s) in a row:\\n\\u003e description-2\",\"short\":false,\"color\":\"#36A64F\",\"fields\":[{\"title\":\"Condition results\",\"value\":\":white_check_mark: - `[CONNECTED] == true`\\n:white_check_mark: - `[STATUS] == 200`\\n\",\"short\":false}]}]}",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			var conditionResults []*endpoint.ConditionResult
			if !scenario.NoConditions {
				conditionResults = []*endpoint.ConditionResult{
					{Condition: "[CONNECTED] == true", Success: scenario.Resolved},
					{Condition: "[STATUS] == 200", Success: scenario.Resolved},
				}
			}
			cfg, err := scenario.Provider.GetConfig(scenario.Endpoint.Group, &scenario.Alert)
			if err != nil {
				t.Fatal("couldn't get config:", err.Error())
			}
			body, _ := json.Marshal(scenario.Provider.buildRequestBody(
				cfg,
				&scenario.Endpoint,
				&scenario.Alert,
				&endpoint.Result{
					ConditionResults: conditionResults,
				},
				scenario.Resolved,
			))
			if string(body) != scenario.ExpectedBody {
				t.Errorf("expected:\n%s\ngot:\n%s", scenario.ExpectedBody, body)
			}
			out := make(map[string]interface{})
			if err := json.Unmarshal(body, &out); err != nil {
				t.Error("expected body to be valid JSON, got error:", err.Error())
			}
		})
	}
}

func TestAlertProvider_GetDefaultAlert(t *testing.T) {
	if (&AlertProvider{DefaultAlert: &alert.Alert{}}).GetDefaultAlert() == nil {
		t.Error("expected default alert to be not nil")
	}
	if (&AlertProvider{DefaultAlert: nil}).GetDefaultAlert() != nil {
		t.Error("expected default alert to be nil")
	}
}

func TestAlertProvider_GetConfig(t *testing.T) {
	scenarios := []struct {
		Name           string
		Provider       AlertProvider
		InputGroup     string
		InputAlert     alert.Alert
		ExpectedOutput Config
	}{
		{
			Name: "provider-no-override-specify-no-group-should-default",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides:     nil,
			},
			InputGroup:     "",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://example.com"},
		},
		{
			Name: "provider-no-override-specify-group-should-default",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides:     nil,
			},
			InputGroup:     "group",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://example.com"},
		},
		{
			Name: "provider-with-override-specify-no-group-should-default",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides: []Override{
					{
						Group:  "group",
						Config: Config{WebhookURL: "http://example01.com"},
					},
				},
			},
			InputGroup:     "",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://example.com"},
		},
		{
			Name: "provider-with-override-specify-group-should-override",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides: []Override{
					{
						Group:  "group",
						Config: Config{WebhookURL: "http://group-example.com"},
					},
				},
			},
			InputGroup:     "group",
			InputAlert:     alert.Alert{},
			ExpectedOutput: Config{WebhookURL: "http://group-example.com"},
		},
		{
			Name: "provider-with-group-override-and-alert-override--alert-override-should-take-precedence",
			Provider: AlertProvider{
				DefaultConfig: Config{WebhookURL: "http://example.com"},
				Overrides: []Override{
					{
						Group:  "group",
						Config: Config{WebhookURL: "http://group-example.com"},
					},
				},
			},
			InputGroup:     "group",
			InputAlert:     alert.Alert{ProviderOverride: map[string]any{"webhook-url": "http://alert-example.com"}},
			ExpectedOutput: Config{WebhookURL: "http://alert-example.com"},
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			got, err := scenario.Provider.GetConfig(scenario.InputGroup, &scenario.InputAlert)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if got.WebhookURL != scenario.ExpectedOutput.WebhookURL {
				t.Errorf("expected webhook URL to be %s, got %s", scenario.ExpectedOutput.WebhookURL, got.WebhookURL)
			}
			// Test ValidateOverrides as well, since it really just calls GetConfig
			if err = scenario.Provider.ValidateOverrides(scenario.InputGroup, &scenario.InputAlert); err != nil {
				t.Errorf("unexpected error: %s", err)
			}
		})
	}
}

func TestAlertProvider_SendWithBotToken(t *testing.T) {
	defer client.InjectHTTPClient(nil)
	provider := AlertProvider{DefaultConfig: Config{BotToken: "xoxb-token", Channel: "#alerts"}}
	scenarios := []struct {
		Name               string
		ResolveKey         string
		Resolved           bool
		Response           string
		ExpectedURL        string
		ExpectedBody       Body
		ExpectedResolveKey string
		ExpectedError      bool
	}{
		{
			Name:               "triggered-posts-message-and-stores-key",
			Response:           `{"ok":true,"channel":"C123","ts":"1700000000.000100"}`,
			ExpectedURL:        "https://slack.com/api/chat.postMessage",
			ExpectedBody:       Body{Channel: "#alerts"},
			ExpectedResolveKey: "C123/1700000000.000100",
		},
		{
			Name:               "resolved-updates-stored-message",
			ResolveKey:         "C123/1700000000.000100",
			Resolved:           true,
			Response:           `{"ok":true,"channel":"C123","ts":"1700000000.000100"}`,
			ExpectedURL:        "https://slack.com/api/chat.update",
			ExpectedBody:       Body{Channel: "C123", TS: "1700000000.000100"},
			ExpectedResolveKey: "",
		},
		{
			Name:         "resolved-without-key-posts-message",
			Resolved:     true,
			Response:     `{"ok":true,"channel":"C123","ts":"1700000000.000200"}`,
			ExpectedURL:  "https://slack.com/api/chat.postMessage",
			ExpectedBody: Body{Channel: "#alerts"},
		},
		{
			Name:          "ok-false-is-an-error",
			Response:      `{"ok":false,"error":"channel_not_found"}`,
			ExpectedURL:   "https://slack.com/api/chat.postMessage",
			ExpectedBody:  Body{Channel: "#alerts"},
			ExpectedError: true,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			var sentBody Body
			client.InjectHTTPClient(&http.Client{Transport: test.MockRoundTripper(func(r *http.Request) *http.Response {
				if r.URL.String() != scenario.ExpectedURL {
					t.Errorf("expected URL %s, got %s", scenario.ExpectedURL, r.URL.String())
				}
				if r.Header.Get("Authorization") != "Bearer xoxb-token" {
					t.Errorf("expected bearer token, got %q", r.Header.Get("Authorization"))
				}
				_ = json.NewDecoder(r.Body).Decode(&sentBody)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(scenario.Response))}
			})})
			a := alert.Alert{ResolveKey: scenario.ResolveKey, SuccessThreshold: 2, FailureThreshold: 2}
			err := provider.Send(&endpoint.Endpoint{Name: "endpoint-name"}, &a, &endpoint.Result{}, scenario.Resolved)
			if scenario.ExpectedError != (err != nil) {
				t.Fatalf("expected error=%v, got %v", scenario.ExpectedError, err)
			}
			if sentBody.Channel != scenario.ExpectedBody.Channel || sentBody.TS != scenario.ExpectedBody.TS {
				t.Errorf("expected channel=%s ts=%s, got channel=%s ts=%s", scenario.ExpectedBody.Channel, scenario.ExpectedBody.TS, sentBody.Channel, sentBody.TS)
			}
			if !scenario.ExpectedError && a.ResolveKey != scenario.ExpectedResolveKey {
				t.Errorf("expected resolve key %q, got %q", scenario.ExpectedResolveKey, a.ResolveKey)
			}
		})
	}
}
