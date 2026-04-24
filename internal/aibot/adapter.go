package aibot

import (
	"context"
	"strings"

	"github.com/pkg/errors"
)

type ExternalTodoRequest struct {
	MemoName string
	Content  string
	Title    string
	Config   ExternalActionAdapter
}

type ExternalTodoResult struct {
	ExternalRef string
	Message     string
}

type ExternalTodoAdapter interface {
	Type() string
	CreateTask(ctx context.Context, request ExternalTodoRequest) (*ExternalTodoResult, error)
}

type TickTickAdapter struct{}

func (a *TickTickAdapter) Type() string {
	return AdapterTypeTickTick
}

func (a *TickTickAdapter) CreateTask(_ context.Context, request ExternalTodoRequest) (*ExternalTodoResult, error) {
	if strings.TrimSpace(request.Config.Secret) == "" {
		return nil, errors.New("ticktick adapter secret is required")
	}
	return nil, errors.New("ticktick adapter is not implemented yet")
}
