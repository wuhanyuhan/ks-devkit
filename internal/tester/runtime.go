package tester

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DetectLanguage 检测项目语言，返回启动命令和参数
func DetectLanguage(projectDir string) (string, []string, error) {
	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err == nil {
		return "go", []string{"run", "."}, nil
	}
	if _, err := os.Stat(filepath.Join(projectDir, "main.py")); err == nil {
		if path, err := exec.LookPath("python3"); err == nil {
			return path, []string{"main.py"}, nil
		}
		return "python", []string{"main.py"}, nil
	}
	return "", nil, fmt.Errorf("无法检测项目语言：未找到 go.mod 或 main.py")
}

// StartAndProbe 启动应用进程并执行运行时探测
func StartAndProbe(projectDir string, port int, timeout time.Duration, manifestTools []string) ([]CheckResult, error) {
	lang, args, err := DetectLanguage(projectDir)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, lang, args...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("KS_APP_PORT=%d", port))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动应用失败: %w", err)
	}

	// 确保进程被清理
	defer func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()

	// 等待端口就绪
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForReady(baseURL+"/healthz", 15*time.Second); err != nil {
		return nil, fmt.Errorf("应用启动超时（15s 内未就绪）: %w", err)
	}

	return ProbeEndpoints(baseURL, manifestTools), nil
}

// waitForReady 轮询 URL 直到返回 200 或超时
func waitForReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("超时等待 %s", url)
}

// ProbeEndpoints 对已运行的应用执行 HTTP 探测（可独立用于测试）
func ProbeEndpoints(baseURL string, manifestTools []string) []CheckResult {
	var results []CheckResult
	client := &http.Client{Timeout: 5 * time.Second}

	// 1. /healthz
	results = append(results, probeGET(client, baseURL+"/healthz", "/healthz", 200))

	// 2. /readyz
	results = append(results, probeGET(client, baseURL+"/readyz", "/readyz", 200))

	// 3. /meta
	results = append(results, probeGET(client, baseURL+"/meta", "/meta", 200))

	// 4. /mcp/tools/list
	resp, err := client.Get(baseURL + "/mcp/tools/list")
	if err != nil {
		results = append(results, CheckResult{Name: "/mcp/tools/list", Passed: false, Message: err.Error()})
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			results = append(results, CheckResult{Name: "/mcp/tools/list", Passed: false, Message: fmt.Sprintf("status %d", resp.StatusCode)})
		} else {
			body, _ := io.ReadAll(resp.Body)
			var toolsResp struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			}
			if err := json.Unmarshal(body, &toolsResp); err != nil {
				results = append(results, CheckResult{Name: "/mcp/tools/list", Passed: false, Message: "响应不是有效 JSON"})
			} else {
				results = append(results, CheckResult{Name: "/mcp/tools/list", Passed: true})

				// 5. 对比 manifest 工具列表
				if len(manifestTools) > 0 {
					runtimeTools := make([]string, len(toolsResp.Tools))
					for i, t := range toolsResp.Tools {
						runtimeTools[i] = t.Name
					}
					sort.Strings(runtimeTools)
					sort.Strings(manifestTools)
					if strings.Join(runtimeTools, ",") != strings.Join(manifestTools, ",") {
						results = append(results, CheckResult{
							Name:    "工具列表一致性",
							Passed:  false,
							Message: fmt.Sprintf("manifest 声明 %v，运行时注册 %v", manifestTools, runtimeTools),
						})
					} else {
						results = append(results, CheckResult{Name: "工具列表一致性", Passed: true})
					}
				}
			}
		}
	}

	// 6. /mcp/tools/call 未知工具 → 期望 404
	reqBody := strings.NewReader(`{"name":"__nonexistent_tool__","params":{}}`)
	callResp, err := client.Post(baseURL+"/mcp/tools/call", "application/json", reqBody)
	if err != nil {
		results = append(results, CheckResult{Name: "/mcp/tools/call 404", Passed: false, Message: err.Error()})
	} else {
		callResp.Body.Close()
		if callResp.StatusCode == 404 {
			results = append(results, CheckResult{Name: "/mcp/tools/call 404", Passed: true})
		} else {
			results = append(results, CheckResult{
				Name:    "/mcp/tools/call 404",
				Passed:  false,
				Message: fmt.Sprintf("期望 404，实际 %d", callResp.StatusCode),
			})
		}
	}

	// 7. MCP Streamable HTTP 协议合规探测
	mcpResults := ProbeMCP(client, baseURL)
	results = append(results, mcpResults...)

	return results
}

func probeGET(client *http.Client, url, name string, expectStatus int) CheckResult {
	resp, err := client.Get(url)
	if err != nil {
		return CheckResult{Name: name, Passed: false, Message: err.Error()}
	}
	resp.Body.Close()
	if resp.StatusCode != expectStatus {
		return CheckResult{Name: name, Passed: false, Message: fmt.Sprintf("期望 %d，实际 %d", expectStatus, resp.StatusCode)}
	}
	return CheckResult{Name: name, Passed: true}
}
