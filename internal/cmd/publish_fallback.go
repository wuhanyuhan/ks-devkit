package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/internal/hub"
	"github.com/wuhanyuhan/ks-devkit/internal/manifest"
)

// runPublishFallback 是 ks publish 在 build 后调用的"manifest 缺字段引导"步骤。
//
// 行为：
//   - jsonOut=true 时直接跳过（NDJSON 模式不能与 stdin prompt 混用）
//   - 否则调 manifest.RunFallbackChain：缺字段则按 LLM → CHANGELOG.md → inline editor 三层引导
//   - 引导后若 spec 改变：写回 manifestPath + 更新 *spec（让后续 UploadVersion 用增强后的 manifest）
//
// 返回 changed 表示是否实际写回了 manifest.yaml；调用方据此决定是否打印"已更新"提示。
//
// repoDir 一般是 "."（当前工作目录）；传参形式为单测能用 t.TempDir()。
func runPublishFallback(
	ctx context.Context,
	client *hub.Client,
	in io.Reader,
	out io.Writer,
	spec *kstypes.AppSpec,
	repoDir string,
	manifestPath string,
	jsonOut bool,
) (bool, error) {
	if jsonOut {
		return false, nil
	}
	skillMd, _ := os.ReadFile(filepath.Join(repoDir, "SKILL.md"))
	enriched, err := manifest.RunFallbackChain(ctx, client, in, out, manifest.FallbackInputs{
		Spec:        *spec,
		SkillMdText: string(skillMd),
		RepoDir:     repoDir,
	})
	if err != nil {
		return false, err
	}
	if reflect.DeepEqual(*spec, enriched) {
		return false, nil
	}
	if err := manifest.WriteManifestYAML(manifestPath, &enriched); err != nil {
		return false, fmt.Errorf("写回 manifest.yaml 失败: %w", err)
	}
	*spec = enriched
	return true, nil
}
