package v1

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/usememos/memos/internal/aibot"
	"github.com/usememos/memos/server/auth"
)

func RegisterAIAssistantRoutes(group *echo.Group, botConfigStore *aibot.ConfigStore, stores *auth.Authenticator) {
	group.GET("/api/v1/ai-assistant", func(c *echo.Context) error {
		if !requireAdmin(c, stores) {
			return c.JSON(http.StatusUnauthorized, map[string]any{"message": "permission denied"})
		}
		config, err := botConfigStore.GetSanitized(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"message": err.Error()})
		}
		return c.JSON(http.StatusOK, config)
	})
	group.PUT("/api/v1/ai-assistant", func(c *echo.Context) error {
		if !requireAdmin(c, stores) {
			return c.JSON(http.StatusUnauthorized, map[string]any{"message": "permission denied"})
		}
		config := aibot.DefaultConfig()
		if err := c.Bind(&config); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"message": "invalid request body"})
		}
		saved, err := botConfigStore.Save(c.Request().Context(), config)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"message": err.Error()})
		}
		return c.JSON(http.StatusOK, saved)
	})
}

func requireAdmin(c *echo.Context, authenticator *auth.Authenticator) bool {
	user, err := authenticator.AuthenticateToUser(c.Request().Context(), c.Request().Header.Get("Authorization"), c.Request().Header.Get("Cookie"))
	if err != nil || user == nil {
		return false
	}
	return user.Role == "ADMIN"
}
