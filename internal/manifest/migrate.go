// Package manifest 的 migrate.go：把旧 manifest 机械迁移到 clean-break 声明形态。
// 用 gopkg.in/yaml.v3 的 yaml.Node 做转换（保留作者注释/字段顺序），不 unmarshal→struct→marshal
// （那会丢注释、重排字段）。每个变换是一个 func(root *yaml.Node, rep *MigrateReport)。
package manifest

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// MigrateOptions 控制迁移行为。
type MigrateOptions struct {
	// TypeOverride 非空时强制 type。squad 仓必须显式传 "squad"——靠 store.team 自动判定不可靠。
	TypeOverride string
}

// MigrateReport 收集需人工跟进的事项（如 requires 映射、install.yaml 配置职责迁移）。
type MigrateReport struct {
	Warnings []string
}

// typeRenameMap：旧四类型 → 新四类型。service/extension 合并为 app；assistant→agent；skill 不变。
var typeRenameMap = map[string]string{
	"service":   "app",
	"extension": "app",
	"assistant": "agent",
	"skill":     "skill",
}

// Migrate 读旧 manifest 字节，返回迁移后的字节（保序保注释）+ report（需人工跟进项）。
func Migrate(raw []byte, opts MigrateOptions) ([]byte, *MigrateReport, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("解析 manifest: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, nil, fmt.Errorf("空 manifest")
	}
	root := doc.Content[0] // mapping node
	rep := &MigrateReport{}
	migrateType(root, opts, rep)
	migrateProvides(root, rep)
	migrateAuth(root, rep)
	migrateManagedResources(root, rep)
	migrateDependencies(root, rep)
	migrateMount(root, rep)
	migrateTopLevel(root, rep)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, nil, fmt.Errorf("编码 manifest: %w", err)
	}
	enc.Close()
	return buf.Bytes(), rep, nil
}

// mapGet 返回 mapping node 里 key 对应的 value node 与其在 Content 中的 key 下标（-1 表示无）。
func mapGet(m *yaml.Node, key string) (*yaml.Node, int) {
	if m == nil {
		return nil, -1
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], i
		}
	}
	return nil, -1
}

// mapDelete 从 mapping node 删除 key（连同其 value）。返回是否删到。
func mapDelete(m *yaml.Node, key string) bool {
	if m == nil {
		return false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

func migrateType(root *yaml.Node, opts MigrateOptions, rep *MigrateReport) {
	v, _ := mapGet(root, "type")
	if v == nil {
		return
	}
	if opts.TypeOverride != "" {
		v.Value = opts.TypeOverride
		return
	}
	if nt, ok := typeRenameMap[v.Value]; ok && nt != v.Value {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("type %q→%q（如为 squad 仓请加 --type=squad 复跑）", v.Value, nt))
		v.Value = nt
	}
}

// bannedCapFields：每个 capability 要砍的废字段。
// 不含 intent_summary/natural_description——那俩先合并进 description 再单独删。
// 保留 input_schema/output_schema（内联 schema 是唯一来源），只砍 *_schema_ref（ref 形态已退役）。
var bannedCapFields = []string{
	"cost_hint", "typical_latency_ms", "input_nl", "output_nl",
	"requires_approval", "allowed_callers", "default_grant",
	"compose_with", "input_schema_ref", "output_schema_ref",
}

// migrateProvides 处理 provides.capabilities[]：canonical_name(带 <id>. 前缀)→裸 name、
// intent_summary+natural_description→description、砍 per-capability 废字段。
// 约一半工具 app 无 provides 段（靠 MCP 动态暴露），无 provides 时直接返回。
func migrateProvides(root *yaml.Node, rep *MigrateReport) {
	idNode, _ := mapGet(root, "id")
	prov, _ := mapGet(root, "provides")
	if prov == nil {
		return
	}
	caps, _ := mapGet(prov, "capabilities")
	if caps == nil || caps.Kind != yaml.SequenceNode {
		return
	}
	prefix := ""
	if idNode != nil {
		prefix = idNode.Value + "."
	}
	for _, capNode := range caps.Content {
		// 1) canonical_name（带前缀）→ name（裸名）。作者只写裸名，全名由 keystone 注册期派生。
		if cn, idx := mapGet(capNode, "canonical_name"); cn != nil {
			bare := cn.Value
			if prefix != "" && strings.HasPrefix(bare, prefix) {
				bare = strings.TrimPrefix(bare, prefix)
			}
			cn.Value = bare
			capNode.Content[idx].Value = "name" // 把 key canonical_name 改成 name
		}
		// 2) intent_summary/natural_description → description（必须在删它俩之前）
		mergeDescription(capNode)
		// 3) 砍 per-capability 废字段 + 已合并的旧 profile 字段
		for _, f := range bannedCapFields {
			mapDelete(capNode, f)
		}
		mapDelete(capNode, "intent_summary")
		mapDelete(capNode, "natural_description")
	}
}

// mergeDescription：若节点无非空 description，用 natural_description（优先）或 intent_summary
// 回填一个 description（追加在字段末尾）。已有 description 时幂等跳过。
func mergeDescription(node *yaml.Node) {
	if d, _ := mapGet(node, "description"); d != nil && strings.TrimSpace(d.Value) != "" {
		return
	}
	src := ""
	if nd, _ := mapGet(node, "natural_description"); nd != nil && strings.TrimSpace(nd.Value) != "" {
		src = nd.Value
	} else if is, _ := mapGet(node, "intent_summary"); is != nil && strings.TrimSpace(is.Value) != "" {
		src = is.Value
	}
	if src == "" {
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "description"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: src},
	)
}

// migrateAuth 把 auth_mode 从 mount.<x>.auth_mode 上提到 top-level auth.mode。
// 上提后残留的 mount.service 死块由 migrateMount 删除（app/squad 走 provides，
// mount 只保留 agent/skill）。
func migrateAuth(root *yaml.Node, rep *MigrateReport) {
	mount, _ := mapGet(root, "mount")
	if mount == nil {
		return
	}
	var mode string
	for i := 0; i+1 < len(mount.Content); i += 2 {
		sub := mount.Content[i+1]
		if am, _ := mapGet(sub, "auth_mode"); am != nil {
			mode = am.Value
			mapDelete(sub, "auth_mode")
		}
	}
	if mode == "" {
		return
	}
	authNode := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "mode"},
		{Kind: yaml.ScalarNode, Value: mode},
	}}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "auth"}, authNode)
}

// bannedResAutoKeys：managed_resources 里值为 "auto" 的样板键（平台自动分配，无需声明）。
var bannedResAutoKeys = []string{"database", "user", "bucket", "prefix", "key_prefix", "collection_prefix"}

// migrateManagedResources 去样板：砍 required（块存在即=需要）、值为 auto 的分配键、
// cache.provider（平台决定）。保留 inject/access/retain/scopes/size 等真实差异化配置。
func migrateManagedResources(root *yaml.Node, rep *MigrateReport) {
	mr, _ := mapGet(root, "managed_resources")
	if mr == nil {
		return
	}
	for i := 0; i+1 < len(mr.Content); i += 2 {
		resName := mr.Content[i].Value
		res := mr.Content[i+1]
		mapDelete(res, "required") // 块存在即=需要
		for _, k := range bannedResAutoKeys {
			if v, _ := mapGet(res, k); v != nil && v.Value == "auto" {
				mapDelete(res, k)
			}
		}
		if resName == "cache" {
			mapDelete(res, "provider") // 缓存后端由平台决定
		}
	}
}

// migrateDependencies 把旧 app 级 dependencies 退役：
//   - requires/recommends（app 级 id+version）无法自动映射到 capability 级 requires.capabilities[]，
//     只发告警让人工改写或删除（能力契约名字即身份、requires 无 version）。
//   - conflicts（app→app）平移到顶层 conflicts.apps，丢弃 version。
//
// 新 conflicts 追加到 root 末尾，再按记录的 idx 删除整个旧 dependencies 块（append 在末尾
// 不影响 idx，故先 append 后 delete 安全）。
func migrateDependencies(root *yaml.Node, rep *MigrateReport) {
	deps, idx := mapGet(root, "dependencies")
	if deps == nil {
		return
	}
	if reqs, _ := mapGet(deps, "requires"); reqs != nil && len(reqs.Content) > 0 {
		rep.Warnings = append(rep.Warnings, "旧 dependencies.requires（app 级 id+version）无法自动映射到 capability 级 requires.capabilities[].canonical_name——请人工改写或删除（能力契约名字即身份、requires 无 version）")
	}
	if recs, _ := mapGet(deps, "recommends"); recs != nil && len(recs.Content) > 0 {
		rep.Warnings = append(rep.Warnings, "旧 dependencies.recommends 同上（app 级），需人工处理")
	}
	if confs, _ := mapGet(deps, "conflicts"); confs != nil && confs.Kind == yaml.SequenceNode && len(confs.Content) > 0 {
		apps := &yaml.Node{Kind: yaml.SequenceNode}
		for _, c := range confs.Content {
			mapDelete(c, "version") // app→app 冲突无 version
			apps.Content = append(apps.Content, c)
		}
		newConf := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "apps"}, apps,
		}}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "conflicts"}, newConf)
	}
	// 删整个旧 dependencies 块（idx 指向 key，append 在末尾未移动它）
	root.Content = append(root.Content[:idx], root.Content[idx+2:]...)
}

// bannedTopLevel：顶层退役段。
//   - license：商业化授权移到平台侧（不在 manifest 声明）。
//   - a2a：声明层不再承载 a2a。
//   - routing_plan：编排官动态决定，不在 manifest 静态声明（全仓零命中，防御性保留）。
//   - protection：系统级保护标记由平台管理。
var bannedTopLevel = []string{"license", "a2a", "routing_plan", "protection"}

// migrateTopLevel 砍顶层死字段/废段：bannedTopLevel + store.media + runtime.port/ports
// （端口由平台分配，manifest 不声明）。
func migrateTopLevel(root *yaml.Node, rep *MigrateReport) {
	for _, k := range bannedTopLevel {
		mapDelete(root, k)
	}
	if store, _ := mapGet(root, "store"); store != nil {
		mapDelete(store, "media")
	}
	if rt, _ := mapGet(root, "runtime"); rt != nil {
		mapDelete(rt, "port")
		mapDelete(rt, "ports")
	}
}

// migrateMount 收敛 mount 段到 v0.30.0 MountSpec（只有 agent/skill 两路）：
//   - agent 类：mount.assistant→mount.agent + 清 profile 旧字段（canonical_name 由平台派生、
//     input_nl/output_nl 等已退役、user_utterances 改 LLM 生成），合并 natural_description→description。
//   - app/squad 类：删 mount.service / mount.extension 死块（能力暴露走 provides，auth_mode 已由
//     migrateAuth 上提到 top-level auth；mcp_endpoint/auto_register_mcp/llm_mode/config_ui 均已退役）。
//   - mount 清空后整段删除。
func migrateMount(root *yaml.Node, rep *MigrateReport) {
	mount, mountIdx := mapGet(root, "mount")
	if mount == nil {
		return
	}
	if asst, idx := mapGet(mount, "assistant"); asst != nil {
		mount.Content[idx].Value = "agent" // 改 key assistant→agent
		if prof, _ := mapGet(asst, "profile"); prof != nil {
			mergeDescription(prof) // 复用 Task 8 的合并逻辑（natural_description→description）
			for _, f := range []string{"canonical_name", "intent_summary", "natural_description", "input_nl", "output_nl", "user_utterances"} {
				mapDelete(prof, f)
			}
		}
	}
	mapDelete(mount, "service")   // app/squad 不用 mount（走 provides），service 块在 v0.30.0 无对应 schema
	mapDelete(mount, "extension") // extension 类型已废
	if len(mount.Content) == 0 {
		root.Content = append(root.Content[:mountIdx], root.Content[mountIdx+2:]...)
	}
}
