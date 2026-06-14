package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestWriteManifestYAML_SingleLocaleCompressed(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "manifest.yaml")
	spec := &kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.1.0", Type: "skill",
		Summary:     kstypes.LocalizedString{"zh-CN": "摘要"},
		Description: kstypes.LocalizedString{"zh-CN": "描述"},
		Tags:        kstypes.LocalizedTags{"zh-CN": []string{"t1", "t2"}},
		Category:    "开发流程",
	}
	if err := WriteManifestYAML(path, spec); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	out := string(data)
	if !strings.Contains(out, "summary: 摘要") {
		t.Errorf("expected single-string summary, got:\n%s", out)
	}
	if !strings.Contains(out, "description: 描述") {
		t.Errorf("expected single-string description, got:\n%s", out)
	}
	if !strings.Contains(out, "- t1") {
		t.Errorf("expected list-form tags, got:\n%s", out)
	}
	if strings.Contains(out, "zh-CN") {
		t.Errorf("single-locale fields should not show zh-CN key, got:\n%s", out)
	}
}

func TestWriteManifestYAML_MultiLocalePreservesMap(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "manifest.yaml")
	spec := &kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.1.0", Type: "skill",
		Summary: kstypes.LocalizedString{"zh-CN": "摘要", "en-US": "Summary"},
	}
	if err := WriteManifestYAML(path, spec); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	out := string(data)
	if !strings.Contains(out, "zh-CN") || !strings.Contains(out, "en-US") {
		t.Errorf("expected map form for multi-locale, got:\n%s", out)
	}
}

func TestWriteManifestYAML_RoundTripParsesBack(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "manifest.yaml")
	spec := &kstypes.AppSpec{
		ID: "skill-tdd", Name: "skill-tdd", Version: "0.3.0", Type: "skill",
		Summary:     kstypes.LocalizedString{"zh-CN": "TDD 助手"},
		Description: kstypes.LocalizedString{"zh-CN": "用 TDD 方法编写代码"},
		Category:    "开发流程",
		Tags:        kstypes.LocalizedTags{"zh-CN": []string{"tdd", "测试"}},
		Changelog:   "### Added\n- 初始版本",
	}
	if err := WriteManifestYAML(path, spec); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	parsed, err := kstypes.ParseAppSpec(data)
	if err != nil {
		t.Fatalf("re-parse failed: %v\n--- written ---\n%s", err, data)
	}
	if parsed.Summary.Get("") != "TDD 助手" {
		t.Errorf("Summary round-trip = %q", parsed.Summary.Get(""))
	}
	got := parsed.Tags.Get("")
	if len(got) != 2 || got[0] != "tdd" || got[1] != "测试" {
		t.Errorf("Tags round-trip = %v", got)
	}
	if parsed.Changelog == "" {
		t.Errorf("Changelog lost in round-trip")
	}
}

func TestWriteManifestYAML_PreservesCapabilityProfile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "manifest.yaml")
	original := []byte(`id: agent-backend-engineer
name: 后端工程师
version: 1.0.0
type: assistant
mount:
  assistant:
    create_agent: true
    name: 后端工程师
    profile:
      canonical_name: agent.backend-engineer
      display_name: 后端工程师
      intent_summary: 后端接口和数据库问题
      natural_description: 适合处理接口契约、慢查询和后端根因分析。
      aliases:
        - 后端工程师
      user_utterances:
        - 找后端工程师看一下
      use_cases:
        - API 接口设计
      domain_terms:
        - 数据库
      input_nl: 需求背景、接口、日志或数据表结构
      output_nl: 接口契约、根因分析和改造建议
      negative_examples:
        - 不替代用户做最终技术选型
`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}

	spec := &kstypes.AppSpec{
		ID: "agent-backend-engineer", Name: "后端工程师", Version: "1.0.0", Type: "assistant",
		Summary:     kstypes.LocalizedString{"zh-CN": "后端工程师，先理数据流。"},
		Description: kstypes.LocalizedString{"zh-CN": "用于后端接口、数据库和稳定性问题。"},
		Category:    "研发工程",
		Tags:        kstypes.LocalizedTags{"zh-CN": []string{"后端开发", "数据库"}},
	}
	if err := WriteManifestYAML(path, spec); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "canonical_name: agent.backend-engineer") {
		t.Fatalf("capability profile canonical_name lost:\n%s", out)
	}
	if !strings.Contains(out, "user_utterances:") {
		t.Fatalf("capability profile user_utterances lost:\n%s", out)
	}
	if !strings.Contains(out, "summary: 后端工程师，先理数据流。") {
		t.Fatalf("updated summary not written:\n%s", out)
	}
}

func TestMarshalManifestJSONForUpload_PreservesCapabilityProfile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "manifest.yaml")
	if err := os.WriteFile(path, []byte(`id: agent-backend-engineer
name: 后端工程师
version: 1.0.0
type: assistant
mount:
  assistant:
    create_agent: true
    name: 后端工程师
    profile:
      canonical_name: agent.backend-engineer
      display_name: 后端工程师
      intent_summary: 后端接口和数据库问题
      natural_description: 适合处理接口契约、慢查询和后端根因分析。
      aliases: [后端工程师]
      user_utterances: [找后端工程师看一下]
      use_cases: [API 接口设计]
      domain_terms: [数据库]
      input_nl: 需求背景、接口、日志或数据表结构
      output_nl: 接口契约、根因分析和改造建议
      negative_examples: [不替代用户做最终技术选型]
`), 0644); err != nil {
		t.Fatal(err)
	}
	spec := &kstypes.AppSpec{
		ID: "agent-backend-engineer", Name: "后端工程师", Version: "1.0.0", Type: "assistant",
		Summary: kstypes.LocalizedString{"zh-CN": "后端工程师，先理数据流。"},
	}

	raw, err := MarshalManifestJSONForUpload(path, spec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	profile := m["mount"].(map[string]any)["assistant"].(map[string]any)["profile"].(map[string]any)
	if profile["canonical_name"] != "agent.backend-engineer" {
		t.Fatalf("canonical_name lost in upload JSON: %s", raw)
	}
	if m["summary"] != "后端工程师，先理数据流。" {
		t.Fatalf("updated summary not reflected in upload JSON: %s", raw)
	}
}

func TestMarshalManifestJSONForUpload_PreservesUnknownRuntimeFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "manifest.yaml")
	if err := os.WriteFile(path, []byte(`id: ks-mcp-sandbox
name: 沙盒执行服务
version: 4.0.0
type: app
runtime:
  mode: container
  image: ks-mcp-sandbox:old
  writable_root_fs: true
  health_check: /health
`), 0644); err != nil {
		t.Fatal(err)
	}
	spec := &kstypes.AppSpec{
		ID:      "ks-mcp-sandbox",
		Name:    "沙盒执行服务",
		Version: "4.0.0",
		Type:    "app",
		Runtime: kstypes.RuntimeSpec{
			Mode:        kstypes.RuntimeModeContainer,
			Image:       "ks-mcp-sandbox:new",
			HealthCheck: "/ready",
		},
	}

	raw, err := MarshalManifestJSONForUpload(path, spec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	runtime := m["runtime"].(map[string]any)
	if runtime["writable_root_fs"] != true {
		t.Fatalf("runtime.writable_root_fs lost in upload JSON: %s", raw)
	}
	if runtime["image"] != "ks-mcp-sandbox:new" {
		t.Fatalf("typed runtime.image should override disk value: %s", raw)
	}
	if runtime["health_check"] != "/ready" {
		t.Fatalf("typed runtime.health_check should override disk value: %s", raw)
	}
}
