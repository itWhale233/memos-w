package aibot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/profile"
	"github.com/usememos/memos/store"
)

const (
	ActionTypeReplyComment = "reply_comment"
	ActionTypeExternalTodo = "external_todo"
	AdapterTypeTickTick    = "ticktick"
)

type ActionSetting struct {
	Enabled   bool   `json:"enabled"`
	Type      string `json:"type"`
	AdapterID string `json:"adapter_id,omitempty"`
}

type ExternalActionAdapter struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Enabled     bool   `json:"enabled"`
	DisplayName string `json:"display_name"`
	Secret      string `json:"secret,omitempty"`
	SecretHint  string `json:"secret_hint,omitempty"`
}

type Config struct {
	Enabled            bool                    `json:"enabled"`
	BotUser            string                  `json:"bot_user"`
	ProviderID         string                  `json:"provider_id"`
	PersonaPrompt      string                  `json:"persona_prompt"`
	SystemPrompt       string                  `json:"system_prompt"`
	TriggerFilter      string                  `json:"trigger_filter"`
	WatchMemoCreate    bool                    `json:"watch_memo_create"`
	WatchMemoUpdate    bool                    `json:"watch_memo_update"`
	WatchCommentCreate bool                    `json:"watch_comment_create"`
	MaxContextComments int32                   `json:"max_context_comments"`
	ClassifyModel      string                  `json:"classify_model"`
	ReplyModel         string                  `json:"reply_model"`
	QuestionAction     ActionSetting           `json:"question_action"`
	EmotionAction      ActionSetting           `json:"emotion_action"`
	TodoAction         ActionSetting           `json:"todo_action"`
	ExternalAdapters   []ExternalActionAdapter `json:"external_action_adapters"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:            false,
		WatchMemoCreate:    true,
		WatchMemoUpdate:    true,
		WatchCommentCreate: true,
		MaxContextComments: 10,
		QuestionAction: ActionSetting{
			Enabled: true,
			Type:    ActionTypeReplyComment,
		},
		EmotionAction: ActionSetting{
			Enabled: true,
			Type:    ActionTypeReplyComment,
		},
		TodoAction: ActionSetting{
			Enabled: true,
			Type:    ActionTypeExternalTodo,
		},
	}
}

type ConfigStore struct {
	profile *profile.Profile
	store   *store.Store
	mu      sync.RWMutex
	cache   Config
}

func NewConfigStore(profile *profile.Profile, store *store.Store) *ConfigStore {
	return &ConfigStore{
		profile: profile,
		store:   store,
		cache:   DefaultConfig(),
	}
}

func (s *ConfigStore) Load(ctx context.Context) (Config, error) {
	s.mu.RLock()
	if s.cache.ProviderID != "" || s.cache.BotUser != "" || s.cache.Enabled {
		cfg := s.cache
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()

	cfg, err := s.readFromDisk()
	if err != nil {
		return Config{}, err
	}
	if err := s.validate(ctx, &cfg); err != nil {
		return Config{}, err
	}
	s.mu.Lock()
	s.cache = cfg
	s.mu.Unlock()
	return cfg, nil
}

func (s *ConfigStore) Save(ctx context.Context, cfg Config) (Config, error) {
	if err := s.validate(ctx, &cfg); err != nil {
		return Config{}, err
	}
	for i := range cfg.ExternalAdapters {
		cfg.ExternalAdapters[i].SecretHint = maskSecret(cfg.ExternalAdapters[i].Secret)
	}
	if err := os.MkdirAll(filepath.Dir(s.configPath()), 0o755); err != nil {
		return Config{}, errors.Wrap(err, "failed to create config directory")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Config{}, errors.Wrap(err, "failed to marshal AI assistant config")
	}
	if err := os.WriteFile(s.configPath(), data, 0o600); err != nil {
		return Config{}, errors.Wrap(err, "failed to write AI assistant config")
	}
	s.mu.Lock()
	s.cache = cfg
	s.mu.Unlock()
	return s.sanitized(cfg), nil
}

func (s *ConfigStore) GetSanitized(ctx context.Context) (Config, error) {
	cfg, err := s.Load(ctx)
	if err != nil {
		return Config{}, err
	}
	return s.sanitized(cfg), nil
}

func (s *ConfigStore) validate(ctx context.Context, cfg *Config) error {
	if cfg.MaxContextComments <= 0 {
		cfg.MaxContextComments = 10
	}
	if cfg.MaxContextComments > 50 {
		return errors.New("max_context_comments must be less than or equal to 50")
	}
	cfg.BotUser = strings.TrimSpace(cfg.BotUser)
	cfg.ProviderID = strings.TrimSpace(cfg.ProviderID)
	cfg.TriggerFilter = strings.TrimSpace(cfg.TriggerFilter)
	for i := range cfg.ExternalAdapters {
		adapter := &cfg.ExternalAdapters[i]
		adapter.ID = strings.TrimSpace(adapter.ID)
		adapter.Type = strings.TrimSpace(adapter.Type)
		adapter.DisplayName = strings.TrimSpace(adapter.DisplayName)
	}
	if !cfg.Enabled {
		return nil
	}
	if cfg.BotUser == "" {
		return errors.New("bot_user is required when enabled")
	}
	if cfg.ProviderID == "" {
		return errors.New("provider_id is required when enabled")
	}
	if strings.TrimSpace(cfg.ReplyModel) == "" {
		return errors.New("reply_model is required when enabled")
	}
	if strings.TrimSpace(cfg.ClassifyModel) == "" {
		cfg.ClassifyModel = cfg.ReplyModel
	}
	user, err := resolveBotUser(ctx, s.store, cfg.BotUser)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("bot_user not found")
	}
	providers, err := s.store.GetInstanceAISetting(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load AI providers")
	}
	found := false
	for _, provider := range providers.GetProviders() {
		if provider != nil && provider.GetId() == cfg.ProviderID {
			found = true
			break
		}
	}
	if !found {
		return errors.Errorf("provider_id %q not found", cfg.ProviderID)
	}
	return nil
}

func (s *ConfigStore) sanitized(cfg Config) Config {
	clone := cfg
	clone.ExternalAdapters = make([]ExternalActionAdapter, 0, len(cfg.ExternalAdapters))
	for _, adapter := range cfg.ExternalAdapters {
		adapter.SecretHint = maskSecret(adapter.Secret)
		adapter.Secret = ""
		clone.ExternalAdapters = append(clone.ExternalAdapters, adapter)
	}
	return clone
}

func (s *ConfigStore) readFromDisk() (Config, error) {
	path := s.configPath()
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, errors.Wrap(err, "failed to read AI assistant config")
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return Config{}, errors.Wrap(err, "failed to unmarshal AI assistant config")
	}
	return cfg, nil
}

func (s *ConfigStore) configPath() string {
	return filepath.Join(s.profile.Data, "config", "ai-assistant.json")
}

func maskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 4 {
		if secret == "" {
			return ""
		}
		return "****"
	}
	return secret[:4] + "..." + secret[len(secret)-2:]
}

func resolveBotUser(ctx context.Context, stores *store.Store, name string) (*store.User, error) {
	username := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "users/"))
	if username == "" {
		return nil, errors.New("bot_user username is required")
	}
	user, err := stores.GetUser(ctx, &store.FindUser{Username: &username})
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve bot user")
	}
	return user, nil
}
