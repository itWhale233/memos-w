package aibot

import (
	"context"
	"log/slog"
	"sync"

	internalbot "github.com/usememos/memos/internal/aibot"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
)

type Runner struct {
	service *internalbot.Service
	queue   chan job
	once    sync.Once
}

type job struct {
	eventType string
	memo      *v1pb.Memo
}

func NewRunner(service *internalbot.Service) *Runner {
	return &Runner{
		service: service,
		queue:   make(chan job, 128),
	}
}

func (r *Runner) Run(ctx context.Context) {
	r.once.Do(func() {
		for range 2 {
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case item := <-r.queue:
						if err := r.service.ProcessMemoEvent(ctx, item.eventType, item.memo); err != nil {
							slog.Warn("AI assistant runner failed", slog.String("eventType", item.eventType), slog.Any("err", err))
						}
					}
				}
			}()
		}
	})
	<-ctx.Done()
}

func (r *Runner) Enqueue(eventType string, memo *v1pb.Memo) {
	if memo == nil {
		return
	}
	select {
	case r.queue <- job{eventType: eventType, memo: memo}:
	default:
		slog.Warn("AI assistant queue is full, dropping event", slog.String("eventType", eventType), slog.String("memo", memo.GetName()))
	}
}
