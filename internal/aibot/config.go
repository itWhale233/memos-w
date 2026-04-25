package aibot

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/profile"
	"github.com/usememos/memos/store"
)

const AdapterTypeTickTick = "ticktick"

type ExternalActionAdapter struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Enabled     bool   `json:"enabled"`
	DisplayName string `json:"display_name"`
	Secret      string `json:"secret,omitempty"`
	SecretHint  string `json:"secret_hint,omitempty"`
}

type RuleGroup struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Tags          []string `json:"tags"`
	PersonaPrompt string   `json:"persona_prompt"`
	SystemPrompt  string   `json:"system_prompt"`
}

type Config struct {
	Enabled             bool                    `json:"enabled"`
	BotUser             string                  `json:"bot_user"`
	ProviderID          string                  `json:"provider_id"`
	RuleGroups          []RuleGroup             `json:"rule_groups"`
	LegacyTriggerFilter string                 `json:"trigger_filter,omitempty"`
	MaxContextComments  int32                   `json:"max_context_comments"`
	ReplyModel          string                  `json:"reply_model"`
	ExternalAdapters    []ExternalActionAdapter `json:"external_action_adapters"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:            false,
		MaxContextComments: 10,
	}
}

type legacyConfig struct {
	Enabled             bool                    `json:"enabled"`
	BotUser             string                  `json:"bot_user"`
	ProviderID          string                  `json:"provider_id"`
	RuleGroups          []RuleGroup             `json:"rule_groups"`
	LegacyTriggerFilter string                 `json:"trigger_filter,omitempty"`
	WatchMemoCreate     bool                    `json:"watch_memo_create"`
	WatchMemoUpdate     bool                    `json:"watch_memo_update"`
	WatchCommentCreate  bool                    `json:"watch_comment_create"`
	MaxContextComments  int32                   `json:"max_context_comments"`
	ReplyModel          string                  `json:"reply_model"`
	ExternalAdapters    []ExternalActionAdapter `json:"external_action_adapters"`
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
		slog.Info("AI assistant config loaded from cache", slog.Bool("enabled", cfg.Enabled), slog.String("provider", cfg.ProviderID), slog.String("bot_user", cfg.BotUser), slog.Int("rule_groups", len(cfg.RuleGroups)))
		return cfg, nil
	}
	s.mu.RUnlock()

	cfg, err := s.readFromDisk()
	if err != nil {
		return Config{}, err
	}
	if err := s.validate(ctx, &cfg); err != nil {
		slog.Warn("AI assistant config validate error", slog.Any("err", err))
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
	if len(cfg.RuleGroups) > 0 {
		seenRuleTags := make(map[string]string)
		normalizedGroups := make([]RuleGroup, 0, len(cfg.RuleGroups))
		for i := range cfg.RuleGroups {
			group := cfg.RuleGroups[i]
			group.ID = strings.TrimSpace(group.ID)
			group.Name = strings.TrimSpace(group.Name)
			group.PersonaPrompt = strings.TrimSpace(group.PersonaPrompt)
			group.SystemPrompt = strings.TrimSpace(group.SystemPrompt)
			group.Tags = normalizeUniqueTags(group.Tags)
			if group.ID == "" {
				group.ID = "rule-" + strings.ReplaceAll(strings.ToLower(group.Name), " ", "-")
			}
			if group.Name == "" {
				return errors.New("rule group name is required")
			}
			if len(group.Tags) == 0 {
				return errors.Errorf("rule group %q must contain at least one tag", group.Name)
			}
			for _, tag := range group.Tags {
				if previousGroup, exists := seenRuleTags[tag]; exists {
					return errors.Errorf("tag %q is already used by rule group %q", tag, previousGroup)
				}
				seenRuleTags[tag] = group.Name
			}
			normalizedGroups = append(normalizedGroups, group)
		}
		cfg.RuleGroups = normalizedGroups
	}
	for i := range cfg.ExternalAdapters {
		adapter := &cfg.ExternalAdapters[i]
		adapter.ID = strings.TrimSpace(adapter.ID)
		adapter.Type = strings.TrimSpace(adapter.Type)
		adapter.DisplayName = strings.TrimSpace(adapter.DisplayName)
	}
	if !cfg.Enabled {
		return nil
	}
	if len(cfg.RuleGroups) == 0 {
		return errors.New("at least one rule group is required when enabled")
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
	clone.RuleGroups = append([]RuleGroup(nil), cfg.RuleGroups...)
	clone.ExternalAdapters = make([]ExternalActionAdapter, 0, len(cfg.ExternalAdapters))
	for _, adapter := range cfg.ExternalAdapters {
		adapter.SecretHint = maskSecret(adapter.Secret)
		adapter.Secret = ""
		clone.ExternalAdapters = append(clone.ExternalAdapters, adapter)
	}
	clone.LegacyTriggerFilter = ""
	return clone
}

func (s *ConfigStore) readFromDisk() (Config, error) {
	path := s.configPath()
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("AI assistant config file not found, using defaults", slog.String("path", path))
			return DefaultConfig(), nil
		}
		return Config{}, errors.Wrap(err, "failed to read AI assistant config")
	}
	slog.Info("AI assistant config file found", slog.String("path", path))
	legacy := legacyConfig{}
	if err := json.Unmarshal(bytes, &legacy); err != nil {
		return Config{}, errors.Wrap(err, "failed to unmarshal AI assistant config")
	}
	cfg := DefaultConfig()
	cfg.Enabled = legacy.Enabled
	cfg.BotUser = legacy.BotUser
	cfg.ProviderID = legacy.ProviderID
	cfg.RuleGroups = legacy.RuleGroups
	cfg.LegacyTriggerFilter = legacy.LegacyTriggerFilter
	cfg.MaxContextComments = legacy.MaxContextComments
	cfg.ReplyModel = legacy.ReplyModel
	cfg.ExternalAdapters = legacy.ExternalAdapters
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

func normalizeUniqueTags(tags []string) []string {
	normalizedTags := make([]string, 0, len(tags))
	seenTags := make(map[string]bool)
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seenTags[tag] {
			continue
		}
		seenTags[tag] = true
		normalizedTags = append(normalizedTags, tag)
	}
	return normalizedTags
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
