package manifest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// reChangelogVersionHeading 匹配 Keep-a-Changelog 风格的版本标题：
//
//	## [0.3.0]
//	## [0.3.0] - 2026-05-02
//	## [0.3.0-beta.1]
//
// 仅匹配带 [ ] 的标准形态，避免误抓 ## v0.3.0 / ## 0.3.0 等其他 markdown 段落标题。
// 多行模式 (?m) 让 ^ 匹配每个行首。
var reChangelogVersionHeading = regexp.MustCompile(`(?m)^##\s+\[(\d+\.\d+\.\d+[\w.-]*)\]`)

// LocalExtractChangelogSection 在给定 markdown 文本中定位 version 对应的 ## [version] section
// 并返回该 section 内容（不含 heading 行，已 trim 首尾空白）。
//
// 匹配规则：严格 Keep-a-Changelog 风格 ## [x.y.z] 或 ## [x.y.z-pre]。
// 没匹配到返回 ("", false) — 调用方应继续走 hub.ParseChangelog 兜底（hub 端规则更宽容）
// 或最终降级到 inline editor。
func LocalExtractChangelogSection(md, version string) (string, bool) {
	indices := reChangelogVersionHeading.FindAllStringSubmatchIndex(md, -1)
	matches := reChangelogVersionHeading.FindAllStringSubmatch(md, -1)
	for i, m := range matches {
		if m[1] != version {
			continue
		}
		// indices[i] = [matchStart, matchEnd, group1Start, group1End]
		start := indices[i][1]
		end := len(md)
		if i+1 < len(indices) {
			end = indices[i+1][0]
		}
		// heading 行末（含可能的 " - 2026-05-02" 日期）跳过
		if nl := strings.IndexByte(md[start:], '\n'); nl >= 0 {
			start += nl + 1
		}
		return strings.TrimSpace(md[start:end]), true
	}
	return "", false
}

// FindChangelogPath 在给定目录下找 CHANGELOG.md 类文件，返回首个存在的绝对路径。
//
// 候选顺序：CHANGELOG.md → CHANGELOG → changelog.md → Changelog.md。
// 找不到返回 ("", false)；调用方据此跳过 fallback 第 2 级，直接走 inline editor。
//
// 注意：macOS / Windows 文件系统大小写不敏感时，前一候选会先命中（即使作者实际叫 changelog.md）。
func FindChangelogPath(dir string) (string, bool) {
	candidates := []string{"CHANGELOG.md", "CHANGELOG", "changelog.md", "Changelog.md"}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}
