package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	routingFactStreamKey    = "gateway_route:{routing_facts}:events"
	routingFactStreamGroup  = "gateway-route-facts-v1"
	routingFactStreamMaxLen = 100000
)

type gatewayRoutingFactStream struct {
	rdb        *redis.Client
	groupMu    sync.Mutex
	groupReady bool
}

func NewGatewayRoutingFactStream(rdb *redis.Client) service.RoutingFactStream {
	return &gatewayRoutingFactStream{rdb: rdb}
}

func (s *gatewayRoutingFactStream) ensureGroup(ctx context.Context) error {
	if s == nil || s.rdb == nil {
		return service.ErrRoutingFactStreamUnavailable
	}
	s.groupMu.Lock()
	defer s.groupMu.Unlock()
	if s.groupReady {
		return nil
	}
	err := s.rdb.XGroupCreateMkStream(ctx, routingFactStreamKey, routingFactStreamGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	s.groupReady = true
	return nil
}

func (s *gatewayRoutingFactStream) Publish(ctx context.Context, payload []byte) error {
	if len(payload) == 0 {
		return errors.New("empty routing fact payload")
	}
	if err := s.ensureGroup(ctx); err != nil {
		return err
	}
	return s.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: routingFactStreamKey, MaxLen: routingFactStreamMaxLen, Approx: true,
		Values: map[string]any{"payload": payload},
	}).Err()
}

func (s *gatewayRoutingFactStream) Read(ctx context.Context, consumer string, count int64, block time.Duration) ([]service.RoutingFactStreamEntry, error) {
	if err := s.ensureGroup(ctx); err != nil {
		return nil, err
	}
	if count <= 0 || count > 256 {
		count = 128
	}
	// Reclaim abandoned pending messages before reading new ones. event_id is
	// unique in PostgreSQL, so ambiguous retry/ack outcomes are idempotent.
	claimed, _, err := s.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: routingFactStreamKey, Group: routingFactStreamGroup, Consumer: consumer,
		MinIdle: 30 * time.Second, Start: "0-0", Count: count,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if len(claimed) > 0 {
		return routingFactEntries(claimed), nil
	}
	streams, err := s.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: routingFactStreamGroup, Consumer: consumer, Streams: []string{routingFactStreamKey, ">"},
		Count: count, Block: block, NoAck: false,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries := make([]service.RoutingFactStreamEntry, 0, count)
	for _, stream := range streams {
		entries = append(entries, routingFactEntries(stream.Messages)...)
	}
	return entries, nil
}

func routingFactEntries(messages []redis.XMessage) []service.RoutingFactStreamEntry {
	entries := make([]service.RoutingFactStreamEntry, 0, len(messages))
	for _, message := range messages {
		value, ok := message.Values["payload"]
		if !ok {
			entries = append(entries, service.RoutingFactStreamEntry{ID: message.ID})
			continue
		}
		var payload []byte
		switch typed := value.(type) {
		case string:
			payload = []byte(typed)
		case []byte:
			payload = append([]byte(nil), typed...)
		default:
			payload = []byte(fmt.Sprint(typed))
		}
		entries = append(entries, service.RoutingFactStreamEntry{ID: message.ID, Payload: payload})
	}
	return entries
}

func (s *gatewayRoutingFactStream) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.ensureGroup(ctx); err != nil {
		return err
	}
	return s.rdb.XAck(ctx, routingFactStreamKey, routingFactStreamGroup, ids...).Err()
}

var _ service.RoutingFactStream = (*gatewayRoutingFactStream)(nil)
