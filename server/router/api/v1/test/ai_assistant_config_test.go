package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/aibot"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
)

func TestAIAssistantConfigRuleGroups(t *testing.T) {
	ctx := context.Background()

	t.Run("save config with unique tags across rule groups", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		hostUser, err := ts.CreateHostUser(ctx, "admin")
		require.NoError(t, err)
		_, err = ts.CreateRegularUser(ctx, "bot")
		require.NoError(t, err)

		adminCtx := ts.CreateUserContext(ctx, hostUser.ID)
		_, err = ts.Service.UpdateInstanceSetting(adminCtx, &v1pb.UpdateInstanceSettingRequest{
			Setting: &v1pb.InstanceSetting{
				Name: "instance/settings/AI",
				Value: &v1pb.InstanceSetting_AiSetting{
					AiSetting: &v1pb.InstanceSetting_AISetting{
						Providers: []*v1pb.InstanceSetting_AIProviderConfig{{
							Id:     "openai-main",
							Title:  "OpenAI",
							Type:   v1pb.InstanceSetting_OPENAI,
							ApiKey: "sk-test",
						}},
					},
				},
			},
		})
		require.NoError(t, err)

		configStore := aibot.NewConfigStore(ts.Profile, ts.Store)
		saved, err := configStore.Save(ctx, aibot.Config{
			Enabled:       true,
			BotUser:       "users/bot",
			ProviderID:    "openai-main",
			ReplyModel:    "gpt-4o-mini",
			RuleGroups: []aibot.RuleGroup{
				{
					ID:            "qna",
					Name:          "QnA",
					Tags:          []string{"疑问", "问题"},
					PersonaPrompt: "你是答疑助手",
					SystemPrompt:  "请简明回答",
				},
				{
					ID:            "emotion",
					Name:          "Emotion",
					Tags:          []string{"情绪"},
					PersonaPrompt: "你是安抚助手",
					SystemPrompt:  "请温和回复",
				},
			},
		})
		require.NoError(t, err)
		require.Len(t, saved.RuleGroups, 2)
		require.Equal(t, []string{"疑问", "问题"}, saved.RuleGroups[0].Tags)
	})

	t.Run("reject duplicated tag across rule groups", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		hostUser, err := ts.CreateHostUser(ctx, "admin")
		require.NoError(t, err)
		_, err = ts.CreateRegularUser(ctx, "bot")
		require.NoError(t, err)

		adminCtx := ts.CreateUserContext(ctx, hostUser.ID)
		_, err = ts.Service.UpdateInstanceSetting(adminCtx, &v1pb.UpdateInstanceSettingRequest{
			Setting: &v1pb.InstanceSetting{
				Name: "instance/settings/AI",
				Value: &v1pb.InstanceSetting_AiSetting{
					AiSetting: &v1pb.InstanceSetting_AISetting{
						Providers: []*v1pb.InstanceSetting_AIProviderConfig{{
							Id:     "openai-main",
							Title:  "OpenAI",
							Type:   v1pb.InstanceSetting_OPENAI,
							ApiKey: "sk-test",
						}},
					},
				},
			},
		})
		require.NoError(t, err)

		configStore := aibot.NewConfigStore(ts.Profile, ts.Store)
		_, err = configStore.Save(ctx, aibot.Config{
			Enabled:       true,
			BotUser:       "users/bot",
			ProviderID:    "openai-main",
			ReplyModel:    "gpt-4o-mini",
			RuleGroups: []aibot.RuleGroup{
				{ID: "a", Name: "A", Tags: []string{"疑问"}},
				{ID: "b", Name: "B", Tags: []string{"疑问"}},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "tag \"疑问\" is already used")
	})
}
