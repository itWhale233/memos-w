package aibot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pkg/errors"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/filter"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	"github.com/usememos/memos/store"
)

type MemoCommentCreator interface {
	CreateMemoComment(ctx context.Context, request *v1pb.CreateMemoCommentRequest) (*v1pb.Memo, error)
	ListMemoComments(ctx context.Context, request *v1pb.ListMemoCommentsRequest) (*v1pb.ListMemoCommentsResponse, error)
	ValidateFilter(ctx context.Context, filter string) error
}

type Service struct {
	store       *store.Store
	configStore *ConfigStore
	creator     MemoCommentCreator
	adapters    map[string]ExternalTodoAdapter
}

func NewService(store *store.Store, configStore *ConfigStore, creator MemoCommentCreator) *Service {
	service := &Service{
		store:       store,
		configStore: configStore,
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

func (s *Service) ProcessMemoEvent(ctx context.Context, eventType string, memo *v1pb.Memo) error {
	if memo == nil {
		return nil
	}
	cfg, err := s.configStore.Load(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	if !s.shouldWatchEvent(cfg, eventType, memo.GetParent() != "") {
		return nil
	}
	if memo.GetCreator() == cfg.BotUser {
		return nil
	}
	matched, err := s.matchesFilter(ctx, cfg.TriggerFilter, memo)
	if err != nil {
		return err
	}
	if !matched {
		return nil
	}
	classification := classifyMemo(memo.GetContent())
	if classification == "ignore" {
		return nil
	}
	contextMemos, err := s.loadContext(ctx, cfg, memo)
	if err != nil {
		return err
	}
	switch classification {
	case "question":
		return s.handleReply(ctx, cfg, memo, cfg.QuestionAction, classification, contextMemos)
	case "emotion":
		return s.handleReply(ctx, cfg, memo, cfg.EmotionAction, classification, contextMemos)
	case "todo":
		return s.handleTodo(ctx, cfg, memo, contextMemos)
	default:
		return nil
	}
}

func (s *Service) shouldWatchEvent(cfg Config, eventType string, isComment bool) bool {
	switch eventType {
	case "memo.create":
		if isComment {
			return cfg.WatchCommentCreate
		}
		return cfg.WatchMemoCreate
	case "memo.update":
		return !isComment && cfg.WatchMemoUpdate
	case "memo.comment.create":
		return cfg.WatchCommentCreate
	default:
		return false
	}
}

func (s *Service) matchesFilter(ctx context.Context, filterStr string, memo *v1pb.Memo) (bool, error) {
	if strings.TrimSpace(filterStr) == "" {
		return true, nil
	}
	if err := s.creator.ValidateFilter(ctx, filterStr); err != nil {
		return false, err
	}
	uid := strings.TrimPrefix(memo.GetName(), "memos/")
	find := &store.FindMemo{UID: &uid, ExcludeComments: false, Filters: []string{filterStr}}
	memos, err := s.store.ListMemos(ctx, find)
	if err != nil {
		return false, errors.Wrap(err, "failed to evaluate AI assistant filter")
	}
	return len(memos) > 0, nil
}

func (s *Service) loadContext(ctx context.Context, cfg Config, memo *v1pb.Memo) ([]*v1pb.Memo, error) {
	contextMemos := []*v1pb.Memo{memo}
	parent := memo.GetName()
	if memo.GetParent() != "" {
		parent = memo.GetParent()
	}
	comments, err := s.creator.ListMemoComments(ctx, &v1pb.ListMemoCommentsRequest{Name: parent, PageSize: cfg.MaxContextComments})
	if err != nil {
		return nil, errors.Wrap(err, "failed to load memo comments for AI assistant")
	}
	for _, comment := range comments.GetMemos() {
		if comment.GetCreator() == cfg.BotUser {
			continue
		}
		contextMemos = append(contextMemos, comment)
	}
	return contextMemos, nil
}

func (s *Service) handleReply(ctx context.Context, cfg Config, memo *v1pb.Memo, action ActionSetting, classification string, contextMemos []*v1pb.Memo) error {
	if !action.Enabled || action.Type != ActionTypeReplyComment {
		return nil
	}
	reply, err := s.generateReply(ctx, cfg, memo, classification, contextMemos)
	if err != nil {
		return err
	}
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	botCtx, err := s.botContext(ctx, cfg.BotUser)
	if err != nil {
		return err
	}
	parent := memo.GetName()
	if memo.GetParent() != "" {
		parent = memo.GetParent()
	}
	_, err = s.creator.CreateMemoComment(botCtx, &v1pb.CreateMemoCommentRequest{
		Name: parent,
		Comment: &v1pb.Memo{
			Content: reply,
		},
	})
	return err
}

func (s *Service) handleTodo(ctx context.Context, cfg Config, memo *v1pb.Memo, contextMemos []*v1pb.Memo) error {
	action := cfg.TodoAction
	if !action.Enabled {
		return nil
	}
	if action.Type == ActionTypeReplyComment {
		return s.handleReply(ctx, cfg, memo, ActionSetting{Enabled: true, Type: ActionTypeReplyComment}, "todo", contextMemos)
	}
	if action.Type != ActionTypeExternalTodo {
		return nil
	}
	adapterConfig, ok := findAdapterConfig(cfg.ExternalAdapters, action.AdapterID)
	if !ok {
		return errors.Errorf("external adapter %q not found", action.AdapterID)
	}
	adapter, ok := s.adapters[adapterConfig.Type]
	if !ok {
		return errors.Errorf("external adapter type %q is not registered", adapterConfig.Type)
	}
	result, err := adapter.CreateTask(ctx, ExternalTodoRequest{
		MemoName: memo.GetName(),
		Title:    extractTodoTitle(memo.GetContent()),
		Content:  memo.GetContent(),
		Config:   adapterConfig,
	})
	if err != nil {
		return err
	}
	if result == nil || strings.TrimSpace(result.Message) == "" {
		return nil
	}
	return s.handleReply(ctx, cfg, memo, ActionSetting{Enabled: true, Type: ActionTypeReplyComment}, "todo", append(contextMemos, &v1pb.Memo{Content: result.Message}))
}

func (s *Service) generateReply(ctx context.Context, cfg Config, memo *v1pb.Memo, classification string, contextMemos []*v1pb.Memo) (string, error) {
	provider, err := s.resolveProvider(ctx, cfg.ProviderID)
	if err != nil {
		return "", err
	}
	// TODO: replace this heuristic generator with a chat/completion implementation when provider support is added.
	_ = provider
	var recent []string
	for _, item := range contextMemos {
		if item == nil || strings.TrimSpace(item.GetContent()) == "" {
			continue
		}
		recent = append(recent, item.GetContent())
	}
	body := strings.Join(recent, "\n\n")
	switch classification {
	case "question":
		return fmt.Sprintf("%s\n\n我理解你的问题了。基于当前内容，我建议先从以下方向排查或补充信息：\n1. 明确问题目标和上下文。\n2. 列出你已经尝试过的步骤。\n3. 如果愿意，我可以继续根据后续评论帮你逐步细化。\n\n参考内容：\n%s", strings.TrimSpace(cfg.PersonaPrompt), strings.TrimSpace(body)), nil
	case "emotion":
		return fmt.Sprintf("%s\n\n看起来你现在有一些情绪压力。先不用急着解决所有问题，先把最卡住的一件事拆小一点。你也可以继续在评论里说具体卡点，我会顺着上下文陪你理清。", strings.TrimSpace(cfg.PersonaPrompt)), nil
	case "todo":
		return fmt.Sprintf("%s\n\n我已经识别到这更像一条待办事项。建议补充截止时间、优先级和完成标准，后续更方便继续跟进。", strings.TrimSpace(cfg.PersonaPrompt)), nil
	default:
		return "", nil
	}
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
	return context.WithValue(ctx, botContextKey{}, user.ID), nil
}

type botContextKey struct{}

func BotUserIDFromContext(ctx context.Context) int32 {
	if v, ok := ctx.Value(botContextKey{}).(int32); ok {
		return v
	}
	return 0
}

func classifyMemo(content string) string {
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

func LogBestEffort(err error, message string) {
	if err != nil {
		slog.Warn(message, slog.Any("err", err))
	}
}

var _ = filter.DefaultEngine
