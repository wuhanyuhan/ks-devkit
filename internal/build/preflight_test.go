package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardExcludeMatchesGitDir(t *testing.T) {
	cases := []string{
		".git",
		".git/HEAD",
		"sub/.git",
		"a/b/.git/refs/heads/main",
	}
	for _, p := range cases {
		if !isHardExcluded(p) {
			t.Errorf("expected %q to be hard-excluded", p)
		}
	}
}

func TestHardExcludeMatchesEnvFiles(t *testing.T) {
	cases := []string{
		".env",
		".env.local",
		".env.production",
		"sub/.env",
	}
	for _, p := range cases {
		if !isHardExcluded(p) {
			t.Errorf("expected %q to be hard-excluded", p)
		}
	}
}

func TestHardExcludeMatchesKeysAndCerts(t *testing.T) {
	cases := []string{
		"server.pem",
		"client.key",
		"cert.p12",
		"cert.pfx",
		"sub/path/something.pem",
	}
	for _, p := range cases {
		if !isHardExcluded(p) {
			t.Errorf("expected %q to be hard-excluded", p)
		}
	}
}

func TestHardExcludeMatchesSSHIdentities(t *testing.T) {
	cases := []string{
		"id_rsa",
		"id_rsa.pub",
		"id_ed25519",
		".ssh/id_dsa",
		".ssh/id_ecdsa.pub",
	}
	for _, p := range cases {
		if !isHardExcluded(p) {
			t.Errorf("expected %q to be hard-excluded", p)
		}
	}
}

func TestHardExcludeMatchesDepsDirs(t *testing.T) {
	cases := []string{
		"node_modules/foo",
		"vendor/bar",
		"__pycache__/x.pyc",
		"a.pyc",
	}
	for _, p := range cases {
		if !isHardExcluded(p) {
			t.Errorf("expected %q to be hard-excluded", p)
		}
	}
}

func TestHardExcludeMisses(t *testing.T) {
	cases := []string{
		"src/main.go",
		"manifest.yaml",
		"README.md",
		"docs/install.md",
	}
	for _, p := range cases {
		if isHardExcluded(p) {
			t.Errorf("did not expect %q to be hard-excluded", p)
		}
	}
}

func TestRunPreflightHardExcludeRemoves(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "src/main.go"), "package main")
	mustWriteFile(t, filepath.Join(dir, ".env"), "SECRET=x")
	mustWriteFile(t, filepath.Join(dir, ".git/HEAD"), "ref: x")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if hasFile(report.IncludedFiles, "src/main.go") == false {
		t.Fatalf("src/main.go should be included; report = %+v", report)
	}
	if hasFile(report.IncludedFiles, ".env") {
		t.Fatalf(".env should be hard-excluded")
	}
	for _, f := range report.IncludedFiles {
		if strings.HasPrefix(f, ".git/") {
			t.Fatalf(".git/* should be hard-excluded; got %q", f)
		}
	}
	if !report.OK() {
		t.Fatalf("preflight should pass (no fail conditions); report = %+v", report)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func hasFile(list []string, name string) bool {
	for _, x := range list {
		if x == name {
			return true
		}
	}
	return false
}

func TestPreflightHonorsGitignore(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "src/main.go"), "package main")
	mustWriteFile(t, filepath.Join(dir, "build/output.bin"), "binary")
	mustWriteFile(t, filepath.Join(dir, ".gitignore"), "build/\n*.log\n")
	mustWriteFile(t, filepath.Join(dir, "debug.log"), "log")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if hasFile(report.IncludedFiles, "src/main.go") == false {
		t.Fatalf("src/main.go should be included")
	}
	if hasFile(report.IncludedFiles, "build/output.bin") {
		t.Fatalf("build/* should be excluded by .gitignore")
	}
	if hasFile(report.IncludedFiles, "debug.log") {
		t.Fatalf("*.log should be excluded by .gitignore")
	}
}

func TestPreflightHonorsKsignore(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "src/main.go"), "package main")
	mustWriteFile(t, filepath.Join(dir, "testdata/big.json"), "{}")
	mustWriteFile(t, filepath.Join(dir, ".ksignore"), "testdata/\n")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if hasFile(report.IncludedFiles, "testdata/big.json") {
		t.Fatalf("testdata/* should be excluded by .ksignore")
	}
}

func TestPreflightHardOverridesIgnoreNegate(t *testing.T) {
	// .ksignore 反向规则不能拯救硬排除（.env 永远不能上传）
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "SECRET=x")
	mustWriteFile(t, filepath.Join(dir, ".ksignore"), "!.env\n")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if hasFile(report.IncludedFiles, ".env") {
		t.Fatalf(".env must remain hard-excluded even with !.env in .ksignore")
	}
}

func TestSecretScanDetectsKshPat(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "src/handler.go"),
		"package main\nconst x = \"ksh_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.SecretMatches) == 0 {
		t.Fatalf("expected ksh_pat match")
	}
	if report.SecretMatches[0].Rule != "kshub_pat" {
		t.Fatalf("rule = %q", report.SecretMatches[0].Rule)
	}
	if report.SecretMatches[0].File != "src/handler.go" {
		t.Fatalf("file = %q", report.SecretMatches[0].File)
	}
	if report.SecretMatches[0].Line != 2 {
		t.Fatalf("line = %d", report.SecretMatches[0].Line)
	}
	if report.OK() {
		t.Fatalf("OK should be false when secrets detected")
	}
}

func TestSecretScanDetectsAWSAndOpenAI(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "config.json"),
		"{\"aws\":\"AKIAIOSFODNN7EXAMPLE\",\"openai\":\"sk-abcdefghij1234567890ABCDEFGHIJ12345678\"}")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.SecretMatches) < 2 {
		t.Fatalf("expected ≥2 matches; got %d", len(report.SecretMatches))
	}
}

func TestSecretScanSkippedWithAllowFlag(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "src/x.go"),
		"const k = \"ksh_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"")

	report, err := RunPreflight(dir, &PreflightOptions{AllowSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.SecretMatches) != 0 {
		t.Fatalf("AllowSecrets should suppress matches; got %+v", report.SecretMatches)
	}
}

func TestSecretScanIgnoresBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	// 含 NUL 字节 → 视作二进制，跳过扫描
	mustWriteFile(t, filepath.Join(dir, "binary.dat"),
		"ksh_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x00ignored")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.SecretMatches) != 0 {
		t.Fatalf("binary files should be skipped; got %+v", report.SecretMatches)
	}
}

func TestSecretScanDetectsPEM(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "private.txt"),
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----\n")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.SecretMatches) == 0 {
		t.Fatalf("expected PEM match")
	}
}

func TestSizeLimitDefault(t *testing.T) {
	dir := t.TempDir()
	// 写一个超过默认 100MB 的"假大"——靠 truncate 制造稀疏文件，避免实际占盘
	big := filepath.Join(dir, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(101 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	f.Close()

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.SizeLimitHit {
		t.Fatalf("expected SizeLimitHit=true; got %+v", report)
	}
	if report.OK() {
		t.Fatalf("OK should be false")
	}
}

func TestSizeLimitOverride(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "medium.bin")
	f, _ := os.Create(big)
	_ = f.Truncate(50 * 1024 * 1024)
	f.Close()

	// 自定义上限 10MB
	report, err := RunPreflight(dir, &PreflightOptions{SizeLimitBytes: 10 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	if !report.SizeLimitHit {
		t.Fatalf("expected hit; got %+v", report)
	}
}

func TestFileLimitDefault(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 11; i++ {
		mustWriteFile(t, filepath.Join(dir, "f", "x"+strings.Repeat("y", i)+".txt"), "x")
	}
	report, err := RunPreflight(dir, &PreflightOptions{FileLimit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !report.FileLimitHit {
		t.Fatalf("expected FileLimitHit=true; got count=%d limit=5", report.FileCount)
	}
}

// TestRunPreflightDistPrefixOnlyMatchesDistDir 防止 dist 前缀模糊匹配误伤
// dist.go 单文件、distribution/ 子目录等合法名字。
func TestRunPreflightDistPrefixOnlyMatchesDistDir(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "src/main.go"), "package main")
	mustWriteFile(t, filepath.Join(dir, "dist/output.bin"), "x")        // 应跳过
	mustWriteFile(t, filepath.Join(dir, "dist.go"), "package main")     // 不应跳过
	mustWriteFile(t, filepath.Join(dir, "distribution/readme.md"), "x") // 不应跳过

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if hasFile(report.IncludedFiles, "dist/output.bin") {
		t.Errorf("dist/output.bin 应被跳过")
	}
	if !hasFile(report.IncludedFiles, "dist.go") {
		t.Errorf("dist.go 不应被 dist 前缀误伤；included=%v", report.IncludedFiles)
	}
	if !hasFile(report.IncludedFiles, "distribution/readme.md") {
		t.Errorf("distribution/readme.md 不应被 dist 前缀误伤；included=%v", report.IncludedFiles)
	}
}

// TestSecretScanSkipsHeuristicForLockfile 验证 npm package-lock.json 等 lockfile
// 里的 base64 integrity hash 不会触发 generic_high_entropy 启发式规则误报。
func TestSecretScanSkipsHeuristicForLockfile(t *testing.T) {
	// 真实 npm sha512 integrity 字符串（base64 编码 64 字节 hash，>= 88 字符）
	const npmIntegrity = "sha512-VuM3o5+E+CFl+Lc8tnxjvFwh+Bef+r1KfbfeC2RV5R/cAVB7m1A6Lu/dN2Vqi4qBgdcZJYKaVjAa3ZMVZqf4JQ=="
	cases := []struct {
		name     string
		filename string
	}{
		{"npm", "package-lock.json"},
		{"yarn", "yarn.lock"},
		{"pnpm", "pnpm-lock.yaml"},
		{"bun", "bun.lock"},
		{"cargo", "Cargo.lock"},
		{"go", "go.sum"},
		{"composer", "composer.lock"},
		{"gemfile", "Gemfile.lock"},
		{"poetry", "poetry.lock"},
		{"uv", "uv.lock"},
		{"swift", "Package.resolved"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWriteFile(t, filepath.Join(dir, tc.filename),
				"  \"foo\": {\n    \"integrity\": \""+npmIntegrity+"\"\n  }\n")
			report, err := RunPreflight(dir, &PreflightOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range report.SecretMatches {
				if m.Rule == "generic_high_entropy" {
					t.Errorf("%s 不应触发 generic_high_entropy；matches=%+v", tc.filename, report.SecretMatches)
				}
			}
		})
	}
}

// TestSecretScanCatchesPreciseRulesInLockfile 验证 lockfile 内的真 secret（精确规则）
// 仍被检出，避免白名单把真威胁也屏蔽。
func TestSecretScanCatchesPreciseRulesInLockfile(t *testing.T) {
	dir := t.TempDir()
	// 把 ksh_pat token 故意塞到 package-lock.json 里（模拟真 secret 误填场景）
	mustWriteFile(t, filepath.Join(dir, "package-lock.json"),
		"{ \"npm-private-registry-token\": \"ksh_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\" }\n")
	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range report.SecretMatches {
		if m.Rule == "kshub_pat" {
			found = true
		}
	}
	if !found {
		t.Fatalf("lockfile 内的精确规则 kshub_pat 应被检出；matches=%+v", report.SecretMatches)
	}
}

// TestSecretScanGenericHighEntropyOnNonLockfile 验证非 lockfile 文件的 high entropy
// 检测仍正常工作（确认改动没削弱普通文件的扫描）。
func TestSecretScanGenericHighEntropyOnNonLockfile(t *testing.T) {
	dir := t.TempDir()
	const longBase64 = "VuM3o5EhCFlhLc8tnxjvFwhDBefDr1KfbfeC2RV5R5cAVB7m1A6LudN2Vqi4qBg"
	mustWriteFile(t, filepath.Join(dir, "src/handler.go"),
		"package main\nconst k = \""+longBase64+"\"\n")
	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range report.SecretMatches {
		if m.Rule == "generic_high_entropy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("普通源码文件的 high entropy 仍应被检出；matches=%+v", report.SecretMatches)
	}
}

func TestSecretScanGenericHighEntropyIgnoresLongIdentifiersAndPaths(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "src/widget_payload.go"), `package main

const someVeryLongDescriptiveConfigConstantNameForTesting = "some_long_descriptive_config_value"

func TestSomeVeryLongDescriptiveFunctionNameForCoverage() {}
`)
	mustWriteFile(t, filepath.Join(dir, "README.md"),
		"Spec: ~/projects/example/docs/notes/specs/2024-01-01-sample-design-note.md\n")

	report, err := RunPreflight(dir, &PreflightOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range report.SecretMatches {
		if m.Rule == "generic_high_entropy" {
			t.Fatalf("long identifiers and local doc paths should not trigger generic_high_entropy; matches=%+v", report.SecretMatches)
		}
	}
}
