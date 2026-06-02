package ksapp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// EventsMode 控制 EventsClient 走 WS 还是 polling。
type EventsMode string

const (
	EventsModeWS      EventsMode = "ws"
	EventsModePolling EventsMode = "polling"
)

// EventStream 是单个 task 的事件通道。Task.Events() 返其 Channel()。
type EventStream struct {
	taskID string
	ch     chan map[string]any
	closed bool
	mu     sync.Mutex
}

// Channel 返事件接收 channel。
func (s *EventStream) Channel() <-chan map[string]any { return s.ch }

func (s *EventStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// EventsClient 是 caller-side 的 inbound 事件通道客户端。
//
// 设计：
//   - mode=ws：长连 /v1/apps/self/events，指数退避重连；忽略心跳事件
//   - mode=polling：每 pollInterval 轮询 /v1/apps/self/events?since=<cursor>
//   - 单一 dispatcher 统一把事件按 task_id 路由到对应 EventStream
//
// task_id 不在已注册列表中的事件被静默丢弃（事件可重放）。
type EventsClient struct {
	gatewayURL   string
	appToken     string
	mode         EventsMode
	httpClient   *http.Client
	pollInterval time.Duration

	streamsMu     sync.Mutex
	streams       map[string]*EventStream
	pollingCursor string

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
}

// NewEventsClient 构造一个未启动的 client。需要调 Start(ctx) 才会拉事件。
func NewEventsClient(gatewayURL, appToken string, mode EventsMode) *EventsClient {
	return &EventsClient{
		gatewayURL:   strings.TrimRight(gatewayURL, "/"),
		appToken:     appToken,
		mode:         mode,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		pollInterval: 2 * time.Second,
		streams:      make(map[string]*EventStream),
		stopCh:       make(chan struct{}),
	}
}

// Register 注册某个 task 的事件订阅，返 *EventStream。
// 重复注册同一 task_id 返回同一 stream。
func (c *EventsClient) Register(taskID string) *EventStream {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	if s, ok := c.streams[taskID]; ok {
		return s
	}
	s := &EventStream{taskID: taskID, ch: make(chan map[string]any, 16)}
	c.streams[taskID] = s
	return s
}

// Unregister 撤销订阅并关闭对应 stream channel。
func (c *EventsClient) Unregister(taskID string) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	if s, ok := c.streams[taskID]; ok {
		delete(c.streams, taskID)
		s.close()
	}
}

// dispatch 把事件按 task_id 路由到对应 stream；未注册的静默 drop。
// stream channel 满时丢弃（不阻塞 polling/WS loop）。
func (c *EventsClient) dispatch(event map[string]any) {
	taskID, _ := event["task_id"].(string)
	if taskID == "" {
		return
	}
	c.streamsMu.Lock()
	stream := c.streams[taskID]
	c.streamsMu.Unlock()
	if stream == nil {
		return
	}
	select {
	case stream.ch <- event:
	default:
		// queue 满 — 丢弃（消费方慢，事件可重放）
	}
}

// Start 启动后台 loop（幂等）。
func (c *EventsClient) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		if c.mode == EventsModePolling {
			go c.pollingLoop(ctx)
		} else {
			go c.wsLoop(ctx)
		}
	})
}

// Close 停止 loop 并关闭所有 stream channel。
func (c *EventsClient) Close() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	for _, s := range c.streams {
		s.close()
	}
	c.streams = make(map[string]*EventStream)
}

func (c *EventsClient) pollingLoop(ctx context.Context) {
	tick := time.NewTicker(c.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := c.pollOnce(ctx); err != nil {
				slog.Warn("ksapp: events polling failed", "error", err)
			}
		}
	}
}

// pollOnce GET /v1/apps/self/events?since=<cursor>，把事件 dispatch 到 streams。
// 200 之外的状态码当作 transient 错误（保留 cursor 下次再拉）。
func (c *EventsClient) pollOnce(ctx context.Context) error {
	u := c.gatewayURL + "/v1/apps/self/events"
	if c.pollingCursor != "" {
		u += "?since=" + url.QueryEscape(c.pollingCursor)
	} else {
		u += "?since=0"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.appToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			Events     []map[string]any `json:"events"`
			NextCursor string           `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return err
	}
	for _, ev := range env.Data.Events {
		c.dispatch(ev)
	}
	if env.Data.NextCursor != "" {
		c.pollingCursor = env.Data.NextCursor
	}
	return nil
}

func (c *EventsClient) wsLoop(ctx context.Context) {
	wsURL := strings.Replace(c.gatewayURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/v1/apps/self/events"
	backoff := time.Second
	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, http.Header{
			"Authorization": []string{"Bearer " + c.appToken},
		})
		if err != nil {
			slog.Warn("ksapp: events ws dial failed", "error", err, "backoff", backoff)
			select {
			case <-c.stopCh:
				return
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		c.consumeWS(ctx, conn)
		_ = conn.Close()
	}
}

func (c *EventsClient) consumeWS(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		var ev map[string]any
		if err := conn.ReadJSON(&ev); err != nil {
			return
		}
		if t, _ := ev["type"].(string); t == "heartbeat" {
			continue
		}
		c.dispatch(ev)
	}
}
