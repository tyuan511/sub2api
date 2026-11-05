package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupCheckUsageQueueOverflow(t *testing.T) {
	for _, policy := range []string{"drop", "sample"} {
		for _, gateway := range []string{"anthropic", "openai"} {
			t.Run(policy+"/"+gateway, func(t *testing.T) {
				pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
					WorkerCount: 1, QueueSize: 1, TaskTimeout: time.Second,
					OverflowPolicy: policy, OverflowSamplePercent: 0,
				})
				started, release := make(chan struct{}), make(chan struct{})
				t.Cleanup(func() { close(release); pool.Stop() })
				pool.Submit(func(context.Context) { close(started); <-release })
				<-started
				require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) {}))

				var normal, check atomic.Int32
				submit := func(ctx context.Context, task service.UsageRecordTask) {
					if gateway == "anthropic" {
						(&GatewayHandler{usageRecordWorkerPool: pool}).submitUsageRecordTask(ctx, task)
					} else {
						(&OpenAIGatewayHandler{usageRecordWorkerPool: pool}).submitChatCompletionsUsage(ctx, &service.OpenAIForwardResult{}, task)
					}
				}
				submit(context.Background(), func(context.Context) { normal.Add(1) })
				submit(service.WithGroupCheckPinnedGroup(context.Background(), 42), func(context.Context) { check.Add(1) })
				if policy == "drop" {
					require.EqualValues(t, 0, normal.Load(), "ordinary text requests retain configured drop behavior")
				}
				require.EqualValues(t, 1, check.Load(), "group check usage must execute once on overflow")
			})
		}
	}
}
