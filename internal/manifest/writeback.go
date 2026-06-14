package manifest

import (
	"encoding/json"
	"os"

	kstypes "github.com/wuhanyuhan/ks-types"
	"gopkg.in/yaml.v3"
)

// WriteManifestYAML 把 AppSpec 序列化为 YAML 写到 path。
//
// 重点设计：单 zh-CN locale 形态的 LocalizedString / LocalizedTags 在写回时压缩为
// 单 string / list 形态，避免给只关心中文的作者引入冗余 i18n map：
//
//	# 内存：summary = LocalizedString{"zh-CN": "摘要"}
//	# 写回：summary: 摘要        （而非 summary:\n  zh-CN: 摘要）
//
// 多 locale 仍保留 map 形态：
//
//	summary:
//	  zh-CN: 摘要
//	  en-US: Summary
//
// 如此作者首次 publish 后 git diff manifest.yaml 仍是干净的"加几行字段"，
// 不会出现"已有 summary: TDD 改成 summary:\n  zh-CN: TDD"的格式 churn。
func WriteManifestYAML(path string, spec *kstypes.AppSpec) error {
	m, err := mergedManifestMap(path, spec)
	if err != nil {
		return err
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// MarshalManifestJSONForUpload 返回上传给 hub 的 manifest JSON。
//
// ks publish 内存里的 AppSpec 只承载 ks-types 已知字段；manifest.yaml 可能已经包含
// mount.assistant.profile / mount.skill.profile 等较新的声明字段。上传时必须以磁盘
// manifest 为底稿，再叠加 fallback chain 改动过的标准 metadata 字段，避免 CLI
// 因本地 typed schema 落后而静默丢字段。
func MarshalManifestJSONForUpload(path string, spec *kstypes.AppSpec) ([]byte, error) {
	m, err := mergedManifestMap(path, spec)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func mergedManifestMap(path string, spec *kstypes.AppSpec) (map[string]any, error) {
	data, err := yaml.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var typed map[string]any
	if err := yaml.Unmarshal(data, &typed); err != nil {
		return nil, err
	}
	simplifyLocalizedField(typed, "summary")
	simplifyLocalizedField(typed, "description")
	simplifyLocalizedField(typed, "tags")

	base := map[string]any{}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		_ = yaml.Unmarshal(existing, &base)
	}
	if len(base) == 0 {
		return typed, nil
	}
	for key, value := range typed {
		if value == nil {
			continue
		}
		// mount 可能包含当前 ks-types 尚不认识的 profile 子树。fallback chain 不修改
		// mount，因此已有 mount 以磁盘原文为准，避免重序列化时丢字段。
		if key == "mount" {
			if _, exists := base[key]; exists {
				continue
			}
		}
		base[key] = mergeManifestValue(base[key], value)
	}
	return base, nil
}

func mergeManifestValue(baseValue, typedValue any) any {
	baseMap, baseOK := baseValue.(map[string]any)
	typedMap, typedOK := typedValue.(map[string]any)
	if !baseOK || !typedOK {
		return typedValue
	}
	for key, value := range typedMap {
		if value == nil {
			continue
		}
		baseMap[key] = mergeManifestValue(baseMap[key], value)
	}
	return baseMap
}

// simplifyLocalizedField 把单 zh-CN entry 的 map 压缩成裸值。
// 适用于 LocalizedString（map[string]any 的 string value）与 LocalizedTags（slice value）。
// 多 locale 或无 zh-CN 时保留原 map。
func simplifyLocalizedField(m map[string]any, key string) {
	sub, ok := m[key].(map[string]any)
	if !ok {
		return
	}
	if len(sub) != 1 {
		return
	}
	if v, ok := sub["zh-CN"]; ok {
		m[key] = v
	}
}
