package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type ChatResponse struct {
	Content string `json:"content"`
}

type ChatDebugInfo struct {
	Provider       string `json:"provider,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	Mode           string `json:"mode,omitempty"`
	Attempt        string `json:"attempt,omitempty"`
	Model          string `json:"model,omitempty"`
	RequestBody    string `json:"request_body,omitempty"`
	ResponseBody   string `json:"response_body,omitempty"`
	FallbackReason string `json:"fallback_reason,omitempty"`
}

type ChatError struct {
	Message string        `json:"message"`
	Debug   ChatDebugInfo `json:"debug"`
}

func (e *ChatError) Error() string {
	return e.Message
}

func AsChatError(err error) (*ChatError, bool) {
	if err == nil {
		return nil, false
	}
	chatErr, ok := err.(*ChatError)
	return chatErr, ok
}

type ChatCompletion interface {
	Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error)
}

type ChatCompletionOption func(*chatCompletionOptions)

type chatCompletionOptions struct {
	httpClient *http.Client
}

func WithChatHTTPClient(client *http.Client) ChatCompletionOption {
	return func(o *chatCompletionOptions) {
		o.httpClient = client
	}
}

func NewChatCompletion(config ProviderConfig, options ...ChatCompletionOption) (ChatCompletion, error) {
	opts := &chatCompletionOptions{
		httpClient: http.DefaultClient,
	}
	for _, o := range options {
		o(opts)
	}

	switch config.Type {
	case ProviderOpenAI:
		return newOpenAIChatCompletion(config, *opts), nil
	case ProviderGemini:
		return newGeminiChatCompletion(config, *opts)
	default:
		return nil, errors.Errorf("unsupported AI provider type: %s", config.Type)
	}
}

// DefaultChatModel returns a sensible default model for the given provider type.
func DefaultChatModel(providerType ProviderType) string {
	switch providerType {
	case ProviderOpenAI:
		return "gpt-4o-mini"
	case ProviderGemini:
		return "gemini-2.0-flash"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// OpenAI chat completions
// ---------------------------------------------------------------------------

type openAIChatCompletion struct {
	config     ProviderConfig
	httpClient *http.Client
}

func newOpenAIChatCompletion(config ProviderConfig, opts chatCompletionOptions) *openAIChatCompletion {
	if config.Endpoint == "" {
		config.Endpoint = "https://api.openai.com/v1"
	}
	return &openAIChatCompletion{
		config:     config,
		httpClient: opts.httpClient,
	}
}

func (c *openAIChatCompletion) Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	if request.Model == "" {
		request.Model = DefaultChatModel(ProviderOpenAI)
	}
	return c.chatCompletions(ctx, request)
}

func (c *openAIChatCompletion) chatCompletions(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal openai request")
	}

	endpoint := strings.TrimSuffix(c.config.Endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create openai request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to call openai chat completions")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read openai response")
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &ChatError{
			Message: "openai chat completions returned non-2xx response",
			Debug: ChatDebugInfo{
				Provider:     string(ProviderOpenAI),
				Endpoint:     endpoint,
				Mode:         "chat_completions",
				Attempt:      "primary",
				Model:        request.Model,
				RequestBody:  truncate(payload, 2048),
				ResponseBody: truncate(body, 2048),
			},
		}
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal openai chat response")
	}
	if len(parsed.Choices) == 0 {
		return nil, &ChatError{
			Message: "openai chat completions returned no choices",
			Debug: ChatDebugInfo{
				Provider:     string(ProviderOpenAI),
				Endpoint:     endpoint,
				Mode:         "chat_completions",
				Attempt:      "primary",
				Model:        request.Model,
				RequestBody:  truncate(payload, 2048),
				ResponseBody: truncate(body, 2048),
			},
		}
	}
	content := extractOpenAIContent(parsed.Choices[0].Message.Content)
	if content == "" {
		return nil, &ChatError{
			Message: "openai chat completions returned empty content",
			Debug: ChatDebugInfo{
				Provider:     string(ProviderOpenAI),
				Endpoint:     endpoint,
				Mode:         "chat_completions",
				Attempt:      "primary",
				Model:        request.Model,
				RequestBody:  truncate(payload, 2048),
				ResponseBody: truncate(body, 2048),
			},
		}
	}
	return &ChatResponse{Content: content}, nil
}


// ---------------------------------------------------------------------------
// Gemini chat completions
// ---------------------------------------------------------------------------

type geminiChatCompletion struct {
	config     ProviderConfig
	httpClient *http.Client
}

func newGeminiChatCompletion(config ProviderConfig, opts chatCompletionOptions) (*geminiChatCompletion, error) {
	if config.Endpoint == "" {
		config.Endpoint = "https://generativelanguage.googleapis.com/v1beta"
	}
	if config.APIKey == "" {
		return nil, errors.New("gemini provider API key is required")
	}
	return &geminiChatCompletion{
		config:     config,
		httpClient: opts.httpClient,
	}, nil
}

type geminiPart struct {
	Text string `json:"text"`
}
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

func (c *geminiChatCompletion) Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	model := request.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}
	contents := make([]geminiContent, 0, len(request.Messages))
	for _, msg := range request.Messages {
		role := "user"
		if msg.Role == "assistant" || msg.Role == "model" {
			role = "model"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Content}},
		})
	}
	payload, err := json.Marshal(geminiRequest{Contents: contents})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal gemini request")
	}

	endpoint := strings.TrimSuffix(c.config.Endpoint, "/") + "/models/" + model + ":generateContent?key=" + c.config.APIKey
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gemini request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to call gemini generateContent")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read gemini response")
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, errors.Errorf("gemini generateContent returned status %d: %s", resp.StatusCode, truncate(body, 512))
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal gemini response")
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return nil, errors.New("gemini generateContent returned no candidates")
	}
	return &ChatResponse{Content: parsed.Candidates[0].Content.Parts[0].Text}, nil
}

func truncate(b []byte, max int) string {
	s := string(b)
	if len(s) > max {
		return s[:max]
	}
	return s
}

func extractOpenAIContent(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := extractOpenAIContentPart(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]any:
		// Best effort for unexpected object payloads.
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := []string{}
		for _, key := range keys {
			if text := extractOpenAIContent(v[key]); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func extractOpenAIContentPart(value any) string {
	obj, ok := value.(map[string]any)
	if !ok {
		return extractOpenAIContent(value)
	}
	partType, _ := obj["type"].(string)
	if partType == "text" {
		if text, ok := obj["text"].(string); ok {
			return strings.TrimSpace(text)
		}
	}
	if text, ok := obj["text"].(string); ok {
		return strings.TrimSpace(text)
	}
	return extractOpenAIContent(obj)
}
