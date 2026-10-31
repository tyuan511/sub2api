package service

// Anthropic 协议转换路径的上游 SSE 行泵，用于 OpenAI 侧国产供应商的
// CC×anthropic / Responses×anthropic 转换链，以及 Anthropic 账号的
// chat/responses 兼容入口（anthropicCompatStream）。
//
// 这些转换链的上游 ctx 是
// WithoutCancel（detachStreamUpstreamContext）、http.Client 无整体 Timeout，
// 客户端断开后的排水阶段若上游挂住 SSE（不发数据也不断连），
// scanner.Scan() 将永久阻塞：goroutine 钉死、resp.Body 不归还、连接池位
// 被占用、usage 永不落库。
//
// 与 handleAnthropicStreamingResponse / readOpenAICompatBufferedTerminal 的
// 同类排水一致，本泵用 gateway.stream_data_interval_timeout（默认 180s）作为
// 逐行读间隔上限，超时即向调用方返回 errAnthropicNativeStreamIdle，由调用方
// 关闭 resp.Body 解除阻塞的读并结束排水。

import (
	"bufio"
	"errors"
	"io"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var errAnthropicNativeStreamIdle = errors.New("stream data interval timeout")
var errAnthropicNativeClientGone = errors.New("client gone")

type anthropicNativeLineEvent struct {
	line string
	err  error
}

type anthropicNativeLinePump struct {
	events   chan anthropicNativeLineEvent
	done     chan struct{}
	timer    *time.Timer
	interval time.Duration
}

func newAnthropicNativeLinePump(scanner *bufio.Scanner, interval time.Duration) *anthropicNativeLinePump {
	p := &anthropicNativeLinePump{
		events:   make(chan anthropicNativeLineEvent, 16),
		done:     make(chan struct{}),
		interval: interval,
	}
	if interval > 0 {
		p.timer = time.NewTimer(interval)
	}
	go func() {
		defer close(p.events)
		for scanner.Scan() {
			select {
			case p.events <- anthropicNativeLineEvent{line: scanner.Text()}:
			case <-p.done:
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case p.events <- anthropicNativeLineEvent{err: err}:
			case <-p.done:
			}
		}
	}()
	return p
}

func (p *anthropicNativeLinePump) next() (string, error) {
	return p.nextOrClientGone(nil)
}

func (p *anthropicNativeLinePump) nextOrClientGone(clientGone <-chan struct{}) (string, error) {
	var timeoutCh <-chan time.Time
	if p.timer != nil {
		timeoutCh = p.timer.C
	}
	select {
	case ev, ok := <-p.events:
		return p.deliver(ev, ok)
	case <-timeoutCh:
		select {
		case ev, ok := <-p.events:
			return p.deliver(ev, ok)
		default:
			return "", errAnthropicNativeStreamIdle
		}
	case <-clientGone:
		return "", errAnthropicNativeClientGone
	}
}

func (p *anthropicNativeLinePump) deliver(ev anthropicNativeLineEvent, ok bool) (string, error) {
	if !ok {
		return "", io.EOF
	}
	p.resetTimer()
	return ev.line, ev.err
}

func (p *anthropicNativeLinePump) enableInterval(interval time.Duration) {
	if p.timer != nil || interval <= 0 {
		return
	}
	p.interval = interval
	p.timer = time.NewTimer(interval)
}

func (p *anthropicNativeLinePump) resetTimer() {
	if p.timer == nil {
		return
	}
	if !p.timer.Stop() {
		select {
		case <-p.timer.C:
		default:
		}
	}
	p.timer.Reset(p.interval)
}

func (p *anthropicNativeLinePump) stop() {
	close(p.done)
	if p.timer != nil {
		if !p.timer.Stop() {
			select {
			case <-p.timer.C:
			default:
			}
		}
	}
}

func anthropicNativeStreamInterval(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.Gateway.StreamDataIntervalTimeout > 0 {
		return time.Duration(cfg.Gateway.StreamDataIntervalTimeout) * time.Second
	}
	return 0
}

func (s *OpenAIGatewayService) anthropicNativeStreamInterval() time.Duration {
	if s == nil {
		return 0
	}
	return anthropicNativeStreamInterval(s.cfg)
}
