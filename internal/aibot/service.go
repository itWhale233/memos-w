package aibot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pkg/errors"

	internalai "github.com/usememos/memos/internal/ai"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	"github.com/usememos/memos/store"
)

type MemoCommentCreator interface {
	CreateMemoComment(ctx context.Context, request *v1pb.CreateMemoCommentRequest) (*v1pb.Memo, error)
	ListMemoComments(ctx context.Context, request *v1pb.ListMemoCommentsRequest) (*v1pb.ListMemoCommentsResponse, error)
	GetMemo(ctx context.Context, request *v1pb.GetMemoRequest) (*v1pb.Memo, error)
}

type Service struct {
	store       *store.Store
	configStore *ConfigStore
	logger      *Logger
	creator     MemoCommentCreator
	adapters    map[string]ExternalTodoAdapter
}

type matchedRuleGroup struct {
	ID            string
	Name          string
	Tags          []string
	PersonaPrompt string
	SystemPrompt  string
}

func NewService(store *store.Store, configStore *ConfigStore, logger *Logger, creator MemoCommentCreator) *Service {
	service := &Service{
		store:       store,
		configStore: configStore,
		logger:      logger,
		creator:     creator,
		adapters:    map[string]ExternalTodoAdapter{},
	}
	service.RegisterAdapter(&TickTickAdapter{})
	return service
}

func (s *Service) RegisterAdapter(adapter ExternalTodoAdapter) {
	if adapter == nil {
		return
	}
	s.adapters[adapter.Type()] = adapter
}

func (s *Service) ConvertMemoForBot(ctx context.Context, memo *store.Memo) (*v1pb.Memo, error) {
	if memo == nil {
		return nil, errors.New("memo is nil")
	}
	return s.creator.GetMemo(ctx, &v1pb.GetMemoRequest{Name: "memos/" + memo.UID})
}

func (s *Service) ProcessMemoEvent(ctx context.Context, eventType string, memo *v1pb.Memo) error {
	if memo == nil {
		return nil
	}
	s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "event", Status: "received", EventType: eventType, Memo: memo.GetName(), Message: "received AI assistant event"})
	cfg, err := s.configStore.Load(ctx)
	if err != nil {
		s.log(ctx, LogEntry{Level: LogLevelError, Stage: "config", Status: "failed", EventType: eventType, Memo: memo.GetName(), Message: err.Error()})
		return err
	}
	if !cfg.Enabled {
		s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "config", Status: "skipped", EventType: eventType, Memo: memo.GetName(), Message: "AI assistant disabled"})
		return nil
	}
	if memo.GetCreator() == cfg.BotUser {
		s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "event", Status: "skipped", EventType: eventType, Memo: memo.GetName(), Message: "creator is bot user"})
		return nil
	}
	if botUser, err := resolveBotUser(ctx, s.store, cfg.BotUser); err == nil && botUser != nil {
		if memo.GetCreator() == "users/"+botUser.Username {
			s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "event", Status: "skipped", EventType: eventType, Memo: memo.GetName(), Message: "creator is resolved bot user"})
			return nil
		}
	}
	ruleGroup, matched, matchedSource, err := s.matchRuleGroupForEvent(ctx, cfg, memo)
	if err != nil {
		s.log(ctx, LogEntry{Level: LogLevelError, Stage: "filter", Status: "failed", EventType: eventType, Memo: memo.GetName(), Message: err.Error()})
		return err
	}
	if !matched {
		detail := map[string]any{"memo_tags": memo.GetTags()}
		detail["rule_groups"] = cfg.RuleGroups
		if memo.GetParent() != "" {
			detail["parent_memo"] = memo.GetParent()
		}
		s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "filter", Status: "skipped", EventType: eventType, Memo: memo.GetName(), Message: "memo tags do not match configured rules", Detail: detail})
		return nil
	}
	s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "filter", Status: "ok", EventType: eventType, Memo: memo.GetName(), Message: "matched AI assistant rule group", Detail: map[string]any{"rule_group": ruleGroup, "matched_source": matchedSource}})

	contextMemos, err := s.loadContext(ctx, cfg, memo)
	if err != nil {
		return err
	}
	classification := "rule_group"
	if ruleGroup != nil && strings.TrimSpace(ruleGroup.Name) != "" {
		classification = ruleGroup.Name
	}
	s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "classify", Status: "ok", EventType: eventType, Memo: memo.GetName(), ProviderID: cfg.ProviderID, Model: cfg.ReplyModel, Message: "skipped AI classification and used matched rule group", Detail: map[string]any{"rule_group": ruleGroup}})
	return s.handleReply(ctx, cfg, memo, classification, contextMemos, ruleGroup)
}

func (s *Service) matchesTags(tags []string, memo *v1pb.Memo) bool {
	if len(tags) == 0 {
		return true
	}
	memoTags := make(map[string]bool, len(memo.GetTags()))
	for _, tag := range memo.GetTags() {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		memoTags[tag] = true
	}
	if len(memoTags) == 0 {
		return false
	}
	for _, tag := range tags {
		if memoTags[strings.TrimSpace(tag)] {
			return true
		}
	}
	return false
}

func (s *Service) matchRuleGroup(cfg Config, memo *v1pb.Memo) (*matchedRuleGroup, bool) {
	for _, group := range cfg.RuleGroups {
		if s.matchesTags(group.Tags, memo) {
			return &matchedRuleGroup{
				ID:            group.ID,
				Name:          group.Name,
				Tags:          append([]string(nil), group.Tags...),
				PersonaPrompt: group.PersonaPrompt,
				SystemPrompt:  group.SystemPrompt,
			}, true
		}
	}
	return nil, false
}

func (s *Service) matchRuleGroupForEvent(ctx context.Context, cfg Config, memo *v1pb.Memo) (*matchedRuleGroup, bool, string, error) {
	if ruleGroup, matched := s.matchRuleGroup(cfg, memo); matched {
		return ruleGroup, true, "memo", nil
	}
	parentMemoName := memo.GetParent()
	if parentMemoName == "" {
		memoUID := strings.TrimPrefix(memo.GetName(), "memos/")
		commentMemo, err := s.store.GetMemo(ctx, &store.FindMemo{UID: &memoUID})
		if err == nil && commentMemo != nil {
			commentType := store.MemoRelationComment
			relations, relationErr := s.store.ListMemoRelations(ctx, &store.FindMemoRelation{MemoID: &commentMemo.ID, Type: &commentType})
			if relationErr == nil && len(relations) > 0 {
				parentMemo, parentErr := s.store.GetMemo(ctx, &store.FindMemo{ID: &relations[0].RelatedMemoID})
				if parentErr == nil && parentMemo != nil {
					parentMemoName = "memos/" + parentMemo.UID
				}
			}
		}
	}
	if parentMemoName == "" {
		return nil, false, "", nil
	}
	parentMemo, err := s.creator.GetMemo(ctx, &v1pb.GetMemoRequest{Name: parentMemoName})
	if err != nil {
		return nil, false, "", errors.Wrap(err, "failed to get parent memo for rule-group matching")
	}
	if parentMemo == nil {
		return nil, false, "", nil
	}
	if ruleGroup, matched := s.matchRuleGroup(cfg, parentMemo); matched {
		return ruleGroup, true, "parent_memo", nil
	}
	return nil, false, "", nil
}

func (s *Service) loadContext(ctx context.Context, cfg Config, memo *v1pb.Memo) ([]*v1pb.Memo, error) {
	contextMemos := []*v1pb.Memo{memo}
	target := memo.GetName()
	if memo.GetParent() != "" {
		target = memo.GetParent()
	}
	comments, err := s.creator.ListMemoComments(ctx, &v1pb.ListMemoCommentsRequest{Name: target, PageSize: cfg.MaxContextComments})
	if err != nil {
		return nil, errors.Wrap(err, "failed to load memo comments for AI assistant")
	}
	for _, comment := range comments.GetMemos() {
		if comment.GetName() == memo.GetName() {
			continue
		}
		contextMemos = append(contextMemos, comment)
	}
	return contextMemos, nil
}

func (s *Service) handleReply(ctx context.Context, cfg Config, memo *v1pb.Memo, classification string, contextMemos []*v1pb.Memo, ruleGroup *matchedRuleGroup) error {
	reply, err := s.generateReply(ctx, cfg, memo, classification, contextMemos, ruleGroup)
	if err != nil {
		s.log(ctx, LogEntry{Level: LogLevelError, Stage: "reply", Status: "failed", Memo: memo.GetName(), ProviderID: cfg.ProviderID, Model: cfg.ReplyModel, Message: err.Error(), Detail: map[string]any{"classification": classification}})
		return err
	}
	if strings.TrimSpace(reply) == "" {
		s.log(ctx, LogEntry{Level: LogLevelWarn, Stage: "reply", Status: "empty", Memo: memo.GetName(), ProviderID: cfg.ProviderID, Model: cfg.ReplyModel, Message: "AI reply is empty", Detail: map[string]any{"classification": classification}})
		return nil
	}
	botCtx, err := s.botContext(ctx, cfg.BotUser)
	if err != nil {
		return err
	}
	target := memo.GetName()
	if memo.GetParent() != "" {
		target = memo.GetParent()
	}
	_, err = s.creator.CreateMemoComment(botCtx, &v1pb.CreateMemoCommentRequest{
		Name: target,
		Comment: &v1pb.Memo{
			Content:    reply,
			Visibility: memo.GetVisibility(),
		},
	})
	if err != nil {
		s.log(ctx, LogEntry{Level: LogLevelError, Stage: "comment", Status: "failed", Memo: memo.GetName(), Target: target, Message: err.Error(), Detail: map[string]any{"classification": classification}})
		slog.Warn("AI assistant failed to create comment", slog.String("target", target), slog.Any("err", err))
	} else {
		s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "comment", Status: "ok", Memo: memo.GetName(), Target: target, Message: "AI assistant comment created", Detail: map[string]any{"classification": classification}})
		slog.Info("AI assistant comment created", slog.String("target", target))
	}
	return err
}


func (s *Service) generateReply(ctx context.Context, cfg Config, memo *v1pb.Memo, classification string, contextMemos []*v1pb.Memo, ruleGroup *matchedRuleGroup) (string, error) {
	provider, err := s.resolveProvider(ctx, cfg.ProviderID)
	if err != nil {
		return "", err
	}
	completion, err := internalai.NewChatCompletion(provider)
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(cfg.ReplyModel)
	if model == "" {
		model = internalai.DefaultChatModel(provider.Type)
	}

	persona := ""
	if ruleGroup != nil && strings.TrimSpace(ruleGroup.PersonaPrompt) != "" {
		persona = strings.TrimSpace(ruleGroup.PersonaPrompt)
	}
	if persona == "" {
		persona = "你是一个温和、克制、实用的中文笔记助手。"
	}
	systemPrompt := ""
	if ruleGroup != nil && strings.TrimSpace(ruleGroup.SystemPrompt) != "" {
		systemPrompt = strings.TrimSpace(ruleGroup.SystemPrompt)
	}
	if systemPrompt == "" {
		systemPrompt = "回答必须准确，不要编造事实；优先短答。回复要求：直接回答，不要重复问题，不要加多余前缀。"
	}

	var contextParts []string
	for _, item := range contextMemos {
		if item == nil || strings.TrimSpace(item.GetContent()) == "" {
			continue
		}
		contextParts = append(contextParts, item.GetContent())
	}
	contextText := strings.Join(contextParts, "\n\n---\n\n")

	userPrompt := fmt.Sprintf("内容类型：%s\n原始内容：\n%s\n\n相关上下文：\n%s", classification, memo.GetContent(), contextText)

	resp, err := completion.Chat(ctx, internalai.ChatRequest{
		Model: model,
		Messages: []internalai.ChatMessage{
			{Role: "system", Content: persona + "\n" + systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		detail := map[string]any{"purpose": "reply", "classification": classification}
		if chatErr, ok := internalai.AsChatError(err); ok {
			detail["provider"] = chatErr.Debug.Provider
			detail["endpoint"] = chatErr.Debug.Endpoint
			detail["mode"] = chatErr.Debug.Mode
			detail["request_body"] = chatErr.Debug.RequestBody
			detail["response_body"] = chatErr.Debug.ResponseBody
			detail["fallback_reason"] = chatErr.Debug.FallbackReason
			s.log(ctx, LogEntry{Level: LogLevelError, Stage: "ai_api", Status: "failed", Memo: memo.GetName(), ProviderID: cfg.ProviderID, Model: model, Message: chatErr.Message, Detail: detail})
		} else {
			s.log(ctx, LogEntry{Level: LogLevelError, Stage: "ai_api", Status: "failed", Memo: memo.GetName(), ProviderID: cfg.ProviderID, Model: model, Message: err.Error(), Detail: detail})
		}
		return "", err
	}
	s.log(ctx, LogEntry{Level: LogLevelInfo, Stage: "ai_api", Status: "ok", Memo: memo.GetName(), ProviderID: cfg.ProviderID, Model: model, Message: "AI reply call succeeded", Detail: map[string]any{"purpose": "reply", "classification": classification, "raw_response": resp.Content}})
	return resp.Content, nil
}

func (s *Service) log(ctx context.Context, entry LogEntry) {
	if s.logger == nil {
		return
	}
	_ = s.logger.Log(ctx, entry)
}

func (s *Service) resolveProvider(ctx context.Context, providerID string) (internalai.ProviderConfig, error) {
	setting, err := s.store.GetInstanceAISetting(ctx)
	if err != nil {
		return internalai.ProviderConfig{}, errors.Wrap(err, "failed to load AI setting")
	}
	providers := make([]internalai.ProviderConfig, 0, len(setting.GetProviders()))
	for _, provider := range setting.GetProviders() {
		if provider == nil {
			continue
		}
		providers = append(providers, internalai.ProviderConfig{
			ID:       provider.GetId(),
			Title:    provider.GetTitle(),
			Type:     internalai.ProviderType(provider.GetType().String()),
			Endpoint: provider.GetEndpoint(),
			APIKey:   provider.GetApiKey(),
		})
	}
	provider, err := internalai.FindProvider(providers, providerID)
	if err != nil {
		return internalai.ProviderConfig{}, errors.Wrap(err, "failed to resolve AI provider")
	}
	return *provider, nil
}

func (s *Service) botContext(ctx context.Context, botUser string) (context.Context, error) {
	user, err := resolveBotUser(ctx, s.store, botUser)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("bot user not found")
	}
	ctx = context.WithValue(ctx, botContextKey{}, user.ID)
	ctx = WithBotVisibilityBypass(ctx)
	return ctx, nil
}

type botContextKey struct{}
type botVisibilityBypassKey struct{}

func BotUserIDFromContext(ctx context.Context) int32 {
	if v, ok := ctx.Value(botContextKey{}).(int32); ok {
		return v
	}
	return 0
}

func WithBotVisibilityBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, botVisibilityBypassKey{}, true)
}

func IsBotVisibilityBypass(ctx context.Context) bool {
	v, ok := ctx.Value(botVisibilityBypassKey{}).(bool)
	return ok && v
}

func classifyMemoByRules(content string) string {
	trimmed := strings.TrimSpace(strings.ToLower(content))
	if trimmed == "" {
		return "ignore"
	}
	if strings.Contains(trimmed, "- [ ]") || strings.Contains(trimmed, "todo") || strings.Contains(trimmed, "待办") {
		return "todo"
	}
	if strings.Contains(trimmed, "?") || strings.Contains(trimmed, "？") || strings.Contains(trimmed, "怎么") || strings.Contains(trimmed, "为什么") {
		return "question"
	}
	emotionKeywords := []string{"难过", "焦虑", "崩溃", "烦", "累", "伤心", "委屈", "stress", "anxious", "sad"}
	for _, keyword := range emotionKeywords {
		if strings.Contains(trimmed, keyword) {
			return "emotion"
		}
	}
	return "ignore"
}

func extractTodoTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "- [ ]"), "-"))
		if line != "" {
			return line
		}
	}
	return "Memos Todo"
}

func findAdapterConfig(adapters []ExternalActionAdapter, id string) (ExternalActionAdapter, bool) {
	for _, adapter := range adapters {
		if adapter.ID == id {
			return adapter, true
		}
	}
	return ExternalActionAdapter{}, false
}
