package tester

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mcpMockOptions 控制 mcpMockServer 各方法的返回行为，nil 表示走默认实现。
// 通过覆盖单个方法可以隔离测试某一个分支，而不需要为每个用例重写整个 switch。
type mcpMockOptions struct {
	initialize func(id int) map[string]any
	toolsList  func(id int) map[string]any
	toolsCall  func(id int, name string) map[string]any
}

// defaultInitialize 返回标准、合规的 initialize 响应。
func defaultInitialize(id int) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"protocolVersion": "2025-03-26",
			"serverInfo":      map[string]any{"name": "mock"},
		},
	}
}

// defaultToolsList 返回包含一个合规工具（含 name + inputSchema）的响应。
func defaultToolsList(id int) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"tools": []map[string]any{
				{
					"name":        "hello",
					"description": "打招呼",
					"inputSchema": map[string]any{"type": "object"},
				},
			},
		},
	}
}

// defaultToolsCall 对未知工具返回 JSON-RPC error，对其他工具返回成功响应。
func defaultToolsCall(id int, name string) map[string]any {
	if name == "__nonexistent__" {
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": -32602, "message": "tool not found"},
		}
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		},
	}
}

// newMCPMockServer 创建一个最小的 MCP Streamable HTTP mock 服务器，
// 支持 initialize / tools/list / tools/call 三个方法的标准响应；
// 通过 opts 可以单独覆盖某个方法的返回值，用于覆盖异常分支。
func newMCPMockServer(opts mcpMockOptions) *httptest.Server {
	if opts.initialize == nil {
		opts.initialize = defaultInitialize
	}
	if opts.toolsList == nil {
		opts.toolsList = defaultToolsList
	}
	if opts.toolsCall == nil {
		opts.toolsCall = defaultToolsCall
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      int            `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		var resp map[string]any
		switch req.Method {
		case "initialize":
			resp = opts.initialize(req.ID)
		case "tools/list":
			resp = opts.toolsList(req.ID)
		case "tools/call":
			name, _ := req.Params["name"].(string)
			resp = opts.toolsCall(req.ID, name)
		default:
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32601, "message": "method not found"},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// findResult 返回结果列表中名称匹配的第一项；找不到时返回 nil。
func findResult(results []CheckResult, name string) *CheckResult {
	for i := range results {
		if results[i].Name == name {
			return &results[i]
		}
	}
	return nil
}

func TestProbeMCP_Happy(t *testing.T) {
	srv := newMCPMockServer(mcpMockOptions{})
	defer srv.Close()

	client := &http.Client{}
	results := ProbeMCP(client, srv.URL)

	for _, r := range results {
		if !r.Passed {
			t.Errorf("%s 未通过: %s", r.Name, r.Message)
		}
	}
	if len(results) != 3 {
		t.Errorf("检查项数量: got %d, want 3", len(results))
	}
}

func TestProbeMCP_NoEndpoint(t *testing.T) {
	client := &http.Client{}
	// 端口 1 通常不可达，确保连接立即失败
	results := ProbeMCP(client, "http://127.0.0.1:1")
	if len(results) == 0 {
		t.Fatal("应至少返回一个失败结果")
	}
	if results[0].Passed {
		t.Error("连接失败时不应通过")
	}
	// initialize 失败应短路，整体只返回一项
	if len(results) != 1 {
		t.Errorf("initialize 失败应短路，got %d 项", len(results))
	}
}

func TestProbeMCP_InitializeMissingProtocolVersion(t *testing.T) {
	srv := newMCPMockServer(mcpMockOptions{
		initialize: func(id int) map[string]any {
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"serverInfo": map[string]any{"name": "mock"},
					// 故意省略 protocolVersion
				},
			}
		},
	})
	defer srv.Close()

	client := &http.Client{}
	results := ProbeMCP(client, srv.URL)

	r := findResult(results, "MCP initialize")
	if r == nil {
		t.Fatal("缺少 MCP initialize 检查项")
	}
	if r.Passed {
		t.Error("缺少 protocolVersion 时不应通过")
	}
	if r.Message != msgInitMissingProtocolVersion {
		t.Errorf("错误消息不符: got %q", r.Message)
	}
}

func TestProbeMCP_InitializeResultNotObject(t *testing.T) {
	srv := newMCPMockServer(mcpMockOptions{
		initialize: func(id int) map[string]any {
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				// result 是数组而不是对象
				"result": []any{1, 2, 3},
			}
		},
	})
	defer srv.Close()

	client := &http.Client{}
	results := ProbeMCP(client, srv.URL)

	r := findResult(results, "MCP initialize")
	if r == nil {
		t.Fatal("缺少 MCP initialize 检查项")
	}
	if r.Passed {
		t.Error("result 不是对象时不应通过")
	}
	if r.Message != msgInitResultNotObject {
		t.Errorf("错误消息不符: got %q, want %q", r.Message, msgInitResultNotObject)
	}
	// initialize 失败应短路，不应触发后续检测
	if len(results) != 1 {
		t.Errorf("initialize 失败应短路，got %d 项", len(results))
	}
}

func TestProbeMCP_InitializeProtocolVersionNull(t *testing.T) {
	srv := newMCPMockServer(mcpMockOptions{
		initialize: func(id int) map[string]any {
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"protocolVersion": nil,
					"serverInfo":      map[string]any{"name": "mock"},
				},
			}
		},
	})
	defer srv.Close()

	client := &http.Client{}
	results := ProbeMCP(client, srv.URL)

	r := findResult(results, "MCP initialize")
	if r == nil {
		t.Fatal("缺少 MCP initialize 检查项")
	}
	if r.Passed {
		t.Error("protocolVersion 为 null 时不应通过")
	}
	if r.Message != msgInitProtocolVersionNull {
		t.Errorf("错误消息不符: got %q, want %q", r.Message, msgInitProtocolVersionNull)
	}
}

func TestProbeMCP_ToolsListNotArray(t *testing.T) {
	srv := newMCPMockServer(mcpMockOptions{
		toolsList: func(id int) map[string]any {
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					// tools 是对象而不是数组——非合规服务端
					"tools": map[string]any{},
				},
			}
		},
	})
	defer srv.Close()

	client := &http.Client{}
	results := ProbeMCP(client, srv.URL)

	r := findResult(results, "MCP tools/list")
	if r == nil {
		t.Fatal("缺少 MCP tools/list 检查项")
	}
	if r.Passed {
		t.Error("tools 不是数组时不应通过（防止假阳性）")
	}
	if r.Message != msgListToolsNotArray {
		t.Errorf("错误消息不符: got %q, want %q", r.Message, msgListToolsNotArray)
	}
}

func TestProbeMCP_ToolsListMissingInputSchema(t *testing.T) {
	srv := newMCPMockServer(mcpMockOptions{
		toolsList: func(id int) map[string]any {
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "hello",
							"description": "缺少 inputSchema 的工具",
							// 故意省略 inputSchema
						},
					},
				},
			}
		},
	})
	defer srv.Close()

	client := &http.Client{}
	results := ProbeMCP(client, srv.URL)

	r := findResult(results, "MCP tools/list")
	if r == nil {
		t.Fatal("缺少 MCP tools/list 检查项")
	}
	if r.Passed {
		t.Error("工具缺少 inputSchema 时不应通过")
	}
	expected := fmt.Sprintf(msgListToolMissingFieldsFmt, 0)
	if r.Message != expected {
		t.Errorf("错误消息不符: got %q, want %q", r.Message, expected)
	}
}

func TestProbeMCP_ToolsCallUnexpectedSuccess(t *testing.T) {
	srv := newMCPMockServer(mcpMockOptions{
		toolsCall: func(id int, name string) map[string]any {
			// 即便调用不存在的工具也回成功，模拟 SDK 错误处理缺失的情况
			return map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "fake success"}},
				},
			}
		},
	})
	defer srv.Close()

	client := &http.Client{}
	results := ProbeMCP(client, srv.URL)

	r := findResult(results, "MCP tools/call 错误处理")
	if r == nil {
		t.Fatal("缺少 MCP tools/call 错误处理 检查项")
	}
	if r.Passed {
		t.Error("调用不存在的工具返回 success 时不应通过")
	}
	if r.Message != msgCallShouldFail {
		t.Errorf("错误消息不符: got %q", r.Message)
	}
}
