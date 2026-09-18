package slack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/TwiN/gatus/v5/alerting/alert"
	"github.com/TwiN/gatus/v5/client"
	"github.com/TwiN/gatus/v5/config/endpoint"
	"gopkg.in/yaml.v3"
)

var (
	ErrWebhookURLNotSet       = errors.New("webhook-url or bot-token and channel must be set")
	ErrDuplicateGroupOverride = errors.New("duplicate group override")
)

type Config struct {
	WebhookURL string `yaml:"webhook-url"`         // Slack webhook URL
	BotToken   string `yaml:"bot-token,omitempty"` // Slack bot token, takes precedence over webhook-url so resolutions update the triggered message
	Channel    string `yaml:"channel,omitempty"`   // Channel to post to when using bot-token
	Title      string `yaml:"title,omitempty"`     // Title of the message that will be sent
}

const apiURL = "https://slack.com/api/"

func (cfg *Config) Validate() error {
	if cfg.usesBot() || len(cfg.WebhookURL) > 0 {
		return nil
	}
	return ErrWebhookURLNotSet
}

func (cfg *Config) usesBot() bool {
	return len(cfg.BotToken) > 0 && len(cfg.Channel) > 0
}

func (cfg *Config) Merge(override *Config) {
	if len(override.WebhookURL) > 0 {
		cfg.WebhookURL = override.WebhookURL
	}
	if len(override.BotToken) > 0 {
		cfg.BotToken = override.BotToken
	}
	if len(override.Channel) > 0 {
		cfg.Channel = override.Channel
	}
	if len(override.Title) > 0 {
		cfg.Title = override.Title
	}
}

// AlertProvider is the configuration necessary for sending an alert using Slack
type AlertProvider struct {
	DefaultConfig Config `yaml:",inline"`

	// DefaultAlert is the default alert configuration to use for endpoints with an alert of the appropriate type
	DefaultAlert *alert.Alert `yaml:"default-alert,omitempty"`

	// Overrides is a list of Override that may be prioritized over the default configuration
	Overrides []Override `yaml:"overrides,omitempty"`
}

// Override is a case under which the default integration is overridden
type Override struct {
	Group  string `yaml:"group"`
	Config `yaml:",inline"`
}

// Validate the provider's configuration
func (provider *AlertProvider) Validate() error {
	registeredGroups := make(map[string]bool)
	if provider.Overrides != nil {
		for _, override := range provider.Overrides {
			if isAlreadyRegistered := registeredGroups[override.Group]; isAlreadyRegistered || override.Group == "" {
				return ErrDuplicateGroupOverride
			}
			registeredGroups[override.Group] = true
		}
	}
	return provider.DefaultConfig.Validate()
}

// Send an alert using the provider
func (provider *AlertProvider) Send(ep *endpoint.Endpoint, alert *alert.Alert, result *endpoint.Result, resolved bool) error {
	cfg, err := provider.GetConfig(ep.Group, alert)
	if err != nil {
		return err
	}
	body := provider.buildRequestBody(cfg, ep, alert, result, resolved)
	url := cfg.WebhookURL
	if cfg.usesBot() {
		url = apiURL + "chat.postMessage"
		if resolved && len(body.TS) > 0 {
			url = apiURL + "chat.update"
		}
	}
	bodyAsJSON, _ := json.Marshal(body)
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(bodyAsJSON))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if cfg.usesBot() {
		request.Header.Set("Authorization", "Bearer "+cfg.BotToken)
	}
	response, err := client.GetHTTPClient(nil).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode > 399 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("call to provider alert returned status code %d: %s", response.StatusCode, string(body))
	}
	if !cfg.usesBot() {
		return nil
	}
	// The Web API reports failures with HTTP 200 and ok=false
	var apiResponse struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	if err := json.NewDecoder(response.Body).Decode(&apiResponse); err != nil {
		return err
	}
	if !apiResponse.OK {
		return fmt.Errorf("call to provider alert returned error: %s", apiResponse.Error)
	}
	if resolved {
		alert.ResolveKey = ""
	} else {
		// Persisted by the watchdog so the resolution can update this message, even across restarts
		alert.ResolveKey = apiResponse.Channel + "/" + apiResponse.TS
	}
	return nil
}

type Body struct {
	Channel     string       `json:"channel,omitempty"`
	TS          string       `json:"ts,omitempty"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

type Attachment struct {
	Title  string  `json:"title"`
	Text   string  `json:"text"`
	Short  bool    `json:"short"`
	Color  string  `json:"color"`
	Fields []Field `json:"fields,omitempty"`
}

type Field struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// buildRequestBody builds the request body for the provider
func (provider *AlertProvider) buildRequestBody(cfg *Config, ep *endpoint.Endpoint, alert *alert.Alert, result *endpoint.Result, resolved bool) Body {
	var message, color string
	if resolved {
		message = fmt.Sprintf("An alert for *%s* has been resolved after passing successfully %d time(s) in a row", ep.DisplayName(), alert.SuccessThreshold)
		color = "#36A64F"
	} else {
		message = fmt.Sprintf("An alert for *%s* has been triggered due to having failed %d time(s) in a row", ep.DisplayName(), alert.FailureThreshold)
		color = "#DD0000"
	}
	var formattedConditionResults string
	for _, conditionResult := range result.ConditionResults {
		var prefix string
		if conditionResult.Success {
			prefix = ":white_check_mark:"
		} else {
			prefix = ":x:"
		}
		formattedConditionResults += fmt.Sprintf("%s - `%s`\n", prefix, conditionResult.Condition)
	}
	var description string
	if alertDescription := alert.GetDescription(); len(alertDescription) > 0 {
		description = ":\n> " + alertDescription
	}
	body := Body{
		Text: "",
		Attachments: []Attachment{
			{
				Title: cfg.Title,
				Text:  message + description,
				Short: false,
				Color: color,
			},
		},
	}
	if cfg.usesBot() {
		body.Channel = cfg.Channel
		// Without a stored message (e.g. triggered before bot-token was configured), post a new one instead
		if channel, ts, found := strings.Cut(alert.ResolveKey, "/"); resolved && found {
			body.Channel, body.TS = channel, ts
		}
	}
	if len(body.Attachments[0].Title) == 0 {
		body.Attachments[0].Title = ":helmet_with_white_cross: Gatus"
	}
	if len(formattedConditionResults) > 0 {
		body.Attachments[0].Fields = append(body.Attachments[0].Fields, Field{
			Title: "Condition results",
			Value: formattedConditionResults,
			Short: false,
		})
	}
	return body
}

// GetDefaultAlert returns the provider's default alert configuration
func (provider *AlertProvider) GetDefaultAlert() *alert.Alert {
	return provider.DefaultAlert
}

// GetConfig returns the configuration for the provider with the overrides applied
func (provider *AlertProvider) GetConfig(group string, alert *alert.Alert) (*Config, error) {
	cfg := provider.DefaultConfig
	// Handle group overrides
	if provider.Overrides != nil {
		for _, override := range provider.Overrides {
			if group == override.Group {
				cfg.Merge(&override.Config)
				break
			}
		}
	}
	// Handle alert overrides
	if len(alert.ProviderOverride) != 0 {
		overrideConfig := Config{}
		if err := yaml.Unmarshal(alert.ProviderOverrideAsBytes(), &overrideConfig); err != nil {
			return nil, err
		}
		cfg.Merge(&overrideConfig)
	}
	// Validate the configuration
	err := cfg.Validate()
	return &cfg, err
}

// ValidateOverrides validates the alert's provider override and, if present, the group override
func (provider *AlertProvider) ValidateOverrides(group string, alert *alert.Alert) error {
	_, err := provider.GetConfig(group, alert)
	return err
}
