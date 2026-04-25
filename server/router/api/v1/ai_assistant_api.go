package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/usememos/memos/internal/aibot"
	"github.com/usememos/memos/server/auth"
	"github.com/usememos/memos/store"
)

func RegisterAIAssistantRoutes(group *echo.Group, botConfigStore *aibot.ConfigStore, botLogger *aibot.Logger, botService *aibot.Service, stores *auth.Authenticator, memoStore *store.Store) {
	group.GET("/api/v1/ai-assistant", func(c *echo.Context) error {
		ctx, ok := requireAdmin(c, stores)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{"message": "permission denied"})
		}
		config, err := botConfigStore.GetSanitized(ctx)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"message": err.Error()})
		}
		return c.JSON(http.StatusOK, config)
	})
	group.GET("/api/v1/ai-assistant/profile", func(c *echo.Context) error {
		config, err := botConfigStore.GetSanitized(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"message": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]any{"bot_user": config.BotUser, "enabled": config.Enabled})
	})
	group.GET("/api/v1/ai-assistant/logs", func(c *echo.Context) error {
		_, ok := requireAdmin(c, stores)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{"message": "permission denied"})
		}
		entries, err := botLogger.ReadRecent(200)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"message": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]any{"logs": entries})
	})
	group.PUT("/api/v1/ai-assistant", func(c *echo.Context) error {
		ctx, ok := requireAdmin(c, stores)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{"message": "permission denied"})
		}
		config := aibot.DefaultConfig()
		if err := c.Bind(&config); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"message": "invalid request body"})
		}
		saved, err := botConfigStore.Save(ctx, config)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"message": err.Error()})
		}
		return c.JSON(http.StatusOK, saved)
	})
	group.POST("/api/v1/ai-assistant/trigger", func(c *echo.Context) error {
		ctx, ok := requireAdmin(c, stores)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{"message": "permission denied"})
		}
		request := struct {
			MemoName string `json:"memo_name"`
		}{}
		if err := c.Bind(&request); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"message": "invalid request body"})
		}
		memoUID, err := ExtractMemoUIDFromName(request.MemoName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"message": "invalid memo name"})
		}
		memo, err := memoStore.GetMemo(ctx, &store.FindMemo{UID: &memoUID})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"message": err.Error()})
		}
		if memo == nil {
			return c.JSON(http.StatusNotFound, map[string]any{"message": "memo not found"})
		}
		memoMessage, err := botService.ConvertMemoForBot(ctx, memo)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"message": err.Error()})
		}
		if err := botService.ProcessMemoEvent(ctx, "memo.manual_trigger", memoMessage); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"message": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]any{"message": "triggered"})
	})
}

func requireAdmin(c *echo.Context, authenticator *auth.Authenticator) (context.Context, bool) {
	user, err := authenticator.AuthenticateToUser(c.Request().Context(), c.Request().Header.Get("Authorization"), c.Request().Header.Get("Cookie"))
	if err != nil || user == nil {
		return c.Request().Context(), false
	}
	if user.Role != "ADMIN" {
		return c.Request().Context(), false
	}
	ctx := auth.SetUserInContext(c.Request().Context(), user, "")
	c.SetRequest(c.Request().WithContext(ctx))
	return ctx, true
}
