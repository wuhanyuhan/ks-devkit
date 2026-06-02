// Package expectations 提供 claimant 端复用的标准断言。
// claimant 调用 RunSuite(t, factory) 跑全量场景。
package expectations

import (
	"context"
	"strings"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/conformance/managed-resources/server"
)

// SelfClientFactory 是 claimant 提供的工厂：根据 baseURL + token 构造一个能调 GET /v1/apps/self/resources 的客户端。
// 返回的对象必须实现 SelfClientUnderTest 接口。
type SelfClientFactory func(baseURL, token string) SelfClientUnderTest

// SelfClientUnderTest 是被测客户端的最小接口。
// keystone 把 secrets 平铺在 env 里（不在 envelope 单独分类），所以套件只验证 env 一个返回值。
// claimant 若实现是 warn-only 策略，需在 Fetch 出错时仍返回 err 非 nil 让套件知道发生错误。
type SelfClientUnderTest interface {
	Fetch(ctx context.Context) (env map[string]string, err error)
}

// RunSuite 运行全套场景。
func RunSuite(t *testing.T, factory SelfClientFactory) {
	t.Helper()

	t.Run("OK", func(t *testing.T) {
		s := server.New(server.ScenarioOK, "tok")
		defer s.Close()
		c := factory(s.URL, "tok")
		env, err := c.Fetch(context.Background())
		if err != nil {
			t.Fatalf("expected no error on OK scenario, got %v", err)
		}
		if env["DB_HOST"] != "managed.example.com" {
			t.Fatalf("expected DB_HOST=managed.example.com, got %q", env["DB_HOST"])
		}
	})

	t.Run("MixedTypes_CoerceString", func(t *testing.T) {
		s := server.New(server.ScenarioMixedTypes, "tok")
		defer s.Close()
		c := factory(s.URL, "tok")
		env, err := c.Fetch(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// JSON number 3306 应被转换为 "3306"
		if env["DB_PORT"] != "3306" {
			t.Fatalf("expected DB_PORT='3306' (string-coerced), got %q", env["DB_PORT"])
		}
		// JSON bool true 应被转换为 "true"
		if env["DEBUG"] != "true" {
			t.Fatalf("expected DEBUG='true' (string-coerced), got %q", env["DEBUG"])
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		s := server.New(server.ScenarioUnauthorized, "tok")
		defer s.Close()
		c := factory(s.URL, "tok")
		_, err := c.Fetch(context.Background())
		if err == nil {
			t.Fatal("expected error on 401, got nil")
		}
	})

	t.Run("ServerError", func(t *testing.T) {
		s := server.New(server.ScenarioServerError, "tok")
		defer s.Close()
		c := factory(s.URL, "tok")
		_, err := c.Fetch(context.Background())
		if err == nil {
			t.Fatal("expected error on 500, got nil")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Fatalf("expected error to mention 500, got: %v", err)
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		s := server.New(server.ScenarioInvalidJSON, "tok")
		defer s.Close()
		c := factory(s.URL, "tok")
		_, err := c.Fetch(context.Background())
		if err == nil {
			t.Fatal("expected error on invalid JSON, got nil")
		}
	})

	t.Run("BusinessError_CodeNonZero", func(t *testing.T) {
		s := server.New(server.ScenarioBusinessError, "tok")
		defer s.Close()
		c := factory(s.URL, "tok")
		_, err := c.Fetch(context.Background())
		if err == nil {
			t.Fatal("expected error on envelope code != 0, got nil")
		}
	})

	t.Run("MissingDataEnv", func(t *testing.T) {
		s := server.New(server.ScenarioMissingDataEnv, "tok")
		defer s.Close()
		c := factory(s.URL, "tok")
		_, err := c.Fetch(context.Background())
		if err == nil {
			t.Fatal("expected error when data.env missing, got nil")
		}
	})
}
