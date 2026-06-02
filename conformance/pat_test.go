// L2 conformance 测试：ks-devkit ↔ ks-hub 真实集成（PAT 路径）。
// 跑前需要先用 docker-compose 启 ks-hub testbed。
//
// 不使用 build tag——通过 KS_HUB_TESTBED_URL / KS_HUB_TESTBED_PAT env 检测；
// 未设置则 t.Skip，CI 默认环境不会跑。

package conformance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireHubURL(t *testing.T) string {
	url := os.Getenv("KS_HUB_TESTBED_URL")
	if url == "" {
		t.Skip("KS_HUB_TESTBED_URL not set; skipping L2 conformance")
	}
	return url
}

func requireTestPAT(t *testing.T) string {
	tok := os.Getenv("KS_HUB_TESTBED_PAT")
	if tok == "" {
		t.Skip("KS_HUB_TESTBED_PAT not set; skipping")
	}
	return tok
}

func TestPATAuthLoginThenPublishFastTrack(t *testing.T) {
	hubURL := requireHubURL(t)
	pat := requireTestPAT(t)

	// 1. ks auth login --token
	cmd := exec.Command("ks", "auth", "login", "--token", pat)
	cmd.Env = append(os.Environ(), "KS_HUB_URL="+hubURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("login failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Saved to") {
		t.Fatalf("login output unexpected:\n%s", out)
	}

	// 2. cd 到 testdata/fast-track-app/ 跑 publish
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(filepath.Join("testdata", "fast-track-app"))

	cmd = exec.Command("ks", "publish")
	cmd.Env = append(os.Environ(), "KS_HUB_URL="+hubURL)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("publish failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "approved") {
		t.Fatalf("expected fast-track approval; got:\n%s", out)
	}
}

func TestPATPublisherMismatchExits2(t *testing.T) {
	requireHubURL(t)
	pat := requireTestPAT(t)
	t.Setenv("KS_HUB_TOKEN", pat)

	// fixture：manifest.publisher = wrong-publisher
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(filepath.Join("testdata", "wrong-publisher-app"))

	cmd := exec.Command("ks", "publish")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("publish should fail; output=%s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if code := exitErr.ExitCode(); code != 2 {
			t.Fatalf("exit code = %d, want 2 (auth/perm)", code)
		}
	}
	if !strings.Contains(string(out), "publisher 错配") {
		t.Fatalf("expected mismatch message; got:\n%s", out)
	}
}

func TestPATSecretInSourceExits3(t *testing.T) {
	requireHubURL(t)
	pat := requireTestPAT(t)
	t.Setenv("KS_HUB_TOKEN", pat)

	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(filepath.Join("testdata", "secret-leak-app"))

	cmd := exec.Command("ks", "publish")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("publish should fail")
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if code := exitErr.ExitCode(); code != 3 {
			t.Fatalf("exit code = %d, want 3", code)
		}
	}
	if !strings.Contains(string(out), "secret detected") {
		t.Fatalf("expected secret detection; got:\n%s", out)
	}
}

// 其它测试用例由后续 PR 补：
// - 撤销 PAT 后 publish exit 2
// - manual 路径 fire-and-forget
// - --no-wait 行为
// - .env 含 PAT 应硬排除（不进 secret 命中）
