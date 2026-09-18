package api

import (
	"net/url"

	"github.com/TwiN/gatus/v5/config"
	"github.com/gofiber/fiber/v2"
)

// requireBadgeVisibility returns 404 for unauthenticated requests on endpoints that are neither public nor opted
// into badges, so private endpoint data can't be read by guessing its key.
func requireBadgeVisibility(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if cfg.Security == nil || cfg.Security.IsAuthenticated(c) {
			return c.Next()
		}
		key, err := url.QueryUnescape(c.Params("key"))
		if err != nil {
			return c.Next()
		}
		if ep := cfg.GetEndpointByKey(key); ep != nil && (ep.Visibility.Public || ep.Visibility.Badges) {
			return c.Next()
		}
		if ee := cfg.GetExternalEndpointByKey(key); ee != nil && (ee.Visibility.Public || ee.Visibility.Badges) {
			return c.Next()
		}
		return c.Status(fiber.StatusNotFound).SendString("endpoint not found")
	}
}
