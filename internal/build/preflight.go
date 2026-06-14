// Package build 的 preflight 实现「上传内容质量门」，采用四层防线设计。
//
// 注意：preflight 是开发者友好性 + 早期 fail-fast 层，不是安全边界。
// 安全边界由服务端 scanner 承担。
package build

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// PreflightOptions 控制 preflight 行为。
type PreflightOptions struct {
	AllowSecrets   bool  // --allow-secrets：跳过第三层 secret 扫描
	SizeLimitBytes int64 // 第四层默认 100MB（0 = 默认）
	FileLimit      int   // 第四层默认 10000（0 = 默认）
	// 注：.gitignore / .ksignore / secret 规则的注入由后续 task 扩展
}

// PreflightReport 是 preflight 的产出。
type PreflightReport struct {
	IncludedFiles  []string      // 通过四层后保留的相对路径
	ExcludedHard   []string      // 第一层硬排除剔除的路径
	ExcludedIgnore []string      // 第二层 .gitignore/.ksignore 剔除的路径（Task 11 填充）
	SecretMatches  []SecretMatch // 第三层命中（Task 12 填充）
	TotalSize      int64         // 第四层度量
	FileCount      int           // 第四层度量
	SizeLimitHit   bool          // 第四层是否超限
	FileLimitHit   bool          // 第四层是否超数量
}

// OK 表示 preflight 没有任何 fail 条件。
// 硬排除和 gitignore 不算 fail（只剔除文件）；secret 命中和体积超限算 fail。
func (r *PreflightReport) OK() bool {
	return len(r.SecretMatches) == 0 && !r.SizeLimitHit && !r.FileLimitHit
}

// SecretMatch 是 secret 扫描的单条命中（Task 12 填充字段）。
type SecretMatch struct {
	File string
	Line int
	Rule string
}

// secretRule 是 secret 扫描的单条规则。
// heuristic=true 表示启发式低置信度规则（如 generic_high_entropy），命中宽泛字符模式，
// 容易在已知 noise 文件（lockfile 的 integrity hash）误报；扫描 lockfile 时会跳过这类规则，
// 但精确规则（如 kshub_pat / aws_access_key_id）仍然扫。
type secretRule struct {
	name      string
	re        *regexp.Regexp
	heuristic bool
}

// secretRules 是 v1 内嵌规则集（约 12 条）。
// 维护：仅命中高置信度形态，避免高误报率。
var secretRules = []secretRule{
	{name: "aws_access_key_id", re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{name: "openai_api_key", re: regexp.MustCompile(`\bsk-[A-Za-z0-9]{32,}\b`)},
	{name: "anthropic_api_key", re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{32,}\b`)},
	{name: "kshub_pat", re: regexp.MustCompile(`\bksh_pat_[a-z0-9]{32}\b`)},
	{name: "github_pat", re: regexp.MustCompile(`\bghp_[A-Za-z0-9]{36}\b`)},
	{name: "github_oauth", re: regexp.MustCompile(`\bgho_[A-Za-z0-9]{36}\b`)},
	{name: "private_key_pem", re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{name: "slack_token", re: regexp.MustCompile(`\bxox[abps]-[A-Za-z0-9-]{10,}\b`)},
	{name: "stripe_key", re: regexp.MustCompile(`\bsk_live_[A-Za-z0-9]{24,}\b`)},
	{name: "jwt_token", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	{name: "database_url", re: regexp.MustCompile(`\b(postgres|mysql|mongodb)://[^:\s]+:[^@\s]+@[^/\s]+`)},
	// generic_high_entropy 启发式：先实现简单版本（长度阈值 + base64 字符集），后续可强化为 Shannon entropy
	{name: "generic_high_entropy", re: regexp.MustCompile(`\b[A-Za-z0-9+/=]{40,}\b`), heuristic: true},
}

// lockfileNames 列出已知 dependency lockfile。这类文件大量包含 base64/hex 编码的 integrity hash
// （npm sha512、Cargo checksum、composer 的 hash 等），会让 generic_high_entropy 启发式规则
// 全面误报。扫描 lockfile 时跳过 heuristic 规则，但精确规则（aws_access_key_id 等）仍然扫，
// 兜底真 secret（例如私有 npm registry token 误填进 package-lock.json 的边角情况）。
var lockfileNames = map[string]bool{
	"package-lock.json": true, // npm
	"yarn.lock":         true, // yarn
	"pnpm-lock.yaml":    true, // pnpm
	"bun.lock":          true, // bun
	"bun.lockb":         true, // bun 二进制 lockfile（实际会被 isLikelyText 先跳过，加上保险）
	"Cargo.lock":        true, // rust cargo
	"go.sum":            true, // go modules
	"composer.lock":     true, // php composer
	"Gemfile.lock":      true, // ruby bundler
	"Pipfile.lock":      true, // python pipenv
	"poetry.lock":       true, // python poetry
	"uv.lock":           true, // python uv
	"pdm.lock":          true, // python pdm
	"mix.lock":          true, // elixir mix
	"Gopkg.lock":        true, // go dep（deprecated）
	"pubspec.lock":      true, // dart/flutter
	"Package.resolved":  true, // swift package manager
}

// isLockfile 用文件 base name 判断是否为已知 dependency lockfile。
func isLockfile(relPath string) bool {
	return lockfileNames[filepath.Base(relPath)]
}

// isLikelyText 嗅探：UTF-8 安全 + 前 8KB 无 NUL 字节即视作文本。
func isLikelyText(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	return !bytes.ContainsRune(buf[:n], 0)
}

// scanForSecrets 扫描单个文件，返回命中列表。
// 已知 dependency lockfile 跳过启发式规则，避免 integrity hash 大面积误报；精确规则仍然扫。
func scanForSecrets(absPath, relPath string) ([]SecretMatch, error) {
	if !isLikelyText(absPath) {
		return nil, nil
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	skipHeuristic := isLockfile(relPath)
	var matches []SecretMatch
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 长行容忍 1MB
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		for _, rule := range secretRules {
			if skipHeuristic && rule.heuristic {
				continue
			}
			if rule.heuristic {
				if lineHasGenericHighEntropyCandidate(rule.re, line) {
					matches = append(matches, SecretMatch{File: relPath, Line: lineNo, Rule: rule.name})
				}
				continue
			}
			if rule.re.Match(line) {
				matches = append(matches, SecretMatch{File: relPath, Line: lineNo, Rule: rule.name})
			}
		}
	}
	return matches, scanner.Err()
}

func lineHasGenericHighEntropyCandidate(re *regexp.Regexp, line []byte) bool {
	for _, token := range re.FindAll(line, -1) {
		if isGenericHighEntropyCandidate(token) {
			return true
		}
	}
	return false
}

func isGenericHighEntropyCandidate(token []byte) bool {
	var hasUpper, hasLower, hasDigit, hasSymbol bool
	slashCount := 0
	for _, b := range token {
		switch {
		case b >= 'A' && b <= 'Z':
			hasUpper = true
		case b >= 'a' && b <= 'z':
			hasLower = true
		case b >= '0' && b <= '9':
			hasDigit = true
		case b == '+' || b == '=':
			hasSymbol = true
		case b == '/':
			slashCount++
		}
	}
	if slashCount > 2 && !hasSymbol {
		return false
	}
	return hasUpper && hasLower && (hasDigit || hasSymbol)
}

// 默认上限
const (
	defaultSizeLimitBytes = 100 * 1024 * 1024
	defaultFileLimit      = 10000
)

// hardExcludePatterns 是绝对不可上传的文件/目录模式列表。
// 实现：路径包含任一 pattern 即匹配。
var hardExcludePatterns = []hardPattern{
	{seg: ".git", typ: dirSegment},
	{seg: ".env", typ: fileExactOrPrefix},
	{seg: ".pem", typ: fileSuffix},
	{seg: ".key", typ: fileSuffix},
	{seg: ".p12", typ: fileSuffix},
	{seg: ".pfx", typ: fileSuffix},
	{seg: "id_rsa", typ: fileExactOrPrefix},
	{seg: "id_dsa", typ: fileExactOrPrefix},
	{seg: "id_ecdsa", typ: fileExactOrPrefix},
	{seg: "id_ed25519", typ: fileExactOrPrefix},
	{seg: ".ssh", typ: dirSegment},
	{seg: ".aws", typ: dirSegment},
	{seg: ".gcp", typ: dirSegment},
	{seg: ".azure", typ: dirSegment},
	{seg: "node_modules", typ: dirSegment},
	{seg: "__pycache__", typ: dirSegment},
	{seg: ".pyc", typ: fileSuffix},
	{seg: ".DS_Store", typ: fileExactOrPrefix},
	{seg: "Thumbs.db", typ: fileExactOrPrefix},
}

type hardPatternType int

const (
	dirSegment        hardPatternType = iota // 任一路径段等于 seg → 命中
	fileExactOrPrefix                        // 文件名 == seg 或 startsWith(seg + ".")
	fileSuffix                               // 文件名 endsWith seg
)

type hardPattern struct {
	seg string
	typ hardPatternType
}

// isHardExcluded 判断 rel 路径是否被硬排除规则命中。rel 是相对项目根的 forward-slash 路径。
func isHardExcluded(rel string) bool {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	last := parts[len(parts)-1]
	for _, p := range hardExcludePatterns {
		switch p.typ {
		case dirSegment:
			for _, seg := range parts {
				if seg == p.seg {
					return true
				}
			}
		case fileExactOrPrefix:
			if last == p.seg || strings.HasPrefix(last, p.seg+".") {
				return true
			}
			// 兼容 ".ssh/foo" 形式：seg 出现在路径段但作为目录头时已在 dirSegment 覆盖
		case fileSuffix:
			if strings.HasSuffix(last, p.seg) {
				return true
			}
		}
	}
	return false
}

// loadIgnoreMatchers 读取 .gitignore + .ksignore，返回合并的 matcher。
// 任一文件不存在时返回基于空规则的 matcher（不会过滤任何文件）。
func loadIgnoreMatchers(projectDir string) (*gitignore.GitIgnore, error) {
	var lines []string
	for _, name := range []string{".gitignore", ".ksignore"} {
		data, err := os.ReadFile(filepath.Join(projectDir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		lines = append(lines, strings.Split(string(data), "\n")...)
	}
	return gitignore.CompileIgnoreLines(lines...), nil
}

// RunPreflight 在 projectDir 上跑 preflight 四层防线，返回 report。
// Task 10 实现第一层；Task 11/12/13/14 增量补齐第二/三/四层和 dry-run。
func RunPreflight(projectDir string, opts *PreflightOptions) (*PreflightReport, error) {
	if opts == nil {
		opts = &PreflightOptions{}
	}
	report := &PreflightReport{}

	matcher, err := loadIgnoreMatchers(projectDir)
	if err != nil {
		return nil, err
	}

	err = filepath.Walk(projectDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(projectDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)

		// 跳过 dist/ 输出目录（精确匹配第一段，避免误伤 dist.go / distribution/）
		if relSlash == "dist" || strings.HasPrefix(relSlash, "dist/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// 第一层：硬排除（最高优先级）
		if isHardExcluded(relSlash) {
			if info.IsDir() {
				report.ExcludedHard = append(report.ExcludedHard, relSlash+"/")
				return filepath.SkipDir
			}
			report.ExcludedHard = append(report.ExcludedHard, relSlash)
			return nil
		}

		// 第二层：.gitignore + .ksignore
		if matcher != nil && matcher.MatchesPath(relSlash) {
			if info.IsDir() {
				report.ExcludedIgnore = append(report.ExcludedIgnore, relSlash+"/")
				return filepath.SkipDir
			}
			report.ExcludedIgnore = append(report.ExcludedIgnore, relSlash)
			return nil
		}

		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		report.IncludedFiles = append(report.IncludedFiles, relSlash)
		report.FileCount++
		report.TotalSize += info.Size()
		if !opts.AllowSecrets {
			found, scanErr := scanForSecrets(path, relSlash)
			if scanErr != nil {
				return scanErr
			}
			report.SecretMatches = append(report.SecretMatches, found...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sizeLimit := opts.SizeLimitBytes
	if sizeLimit == 0 {
		sizeLimit = defaultSizeLimitBytes
	}
	fileLimit := opts.FileLimit
	if fileLimit == 0 {
		fileLimit = defaultFileLimit
	}
	if report.TotalSize > sizeLimit {
		report.SizeLimitHit = true
	}
	if report.FileCount > fileLimit {
		report.FileLimitHit = true
	}

	return report, nil
}
