package build

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// BuildOptions 控制 Build 行为；nil 表示用默认值。
type BuildOptions struct {
	Preflight *PreflightOptions
	DryRun    bool // true：跑完 preflight 但不打包 tarball
}

// BuildResult 描述一次 build 调用的产物信息。
type BuildResult struct {
	AppID       string
	Version     string
	TarballPath string
	TarballSize int64
	Checksum    string
	AppSpec     *kstypes.AppSpec
	InstallSpec *kstypes.InstallSpec // 可选，项目含 install.yaml 时非 nil
}

// Build 读取 projectDir 下的 manifest.yaml，校验后将项目打包为
// gzip tarball 写入 outputDir，并返回带 SHA256 的构建结果。
func Build(projectDir, outputDir string) (*BuildResult, error) {
	// 1. 读取并校验 manifest
	manifestPath := filepath.Join(projectDir, "manifest.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("读取 manifest.yaml 失败: %w", err)
	}

	manifest, err := kstypes.ParseAppSpec(manifestData)
	if err != nil {
		return nil, fmt.Errorf("解析 manifest 失败: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("manifest 校验失败: %w", err)
	}

	// 权限声明校验
	if len(manifest.Permissions) > 0 {
		registry := kstypes.DefaultPermissionRegistry()
		warnings, err := registry.Validate(manifest.Permissions)
		if err != nil {
			return nil, fmt.Errorf("权限声明无效: %w", err)
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "⚠ 权限警告: %s\n", w.Message)
		}
		for _, key := range registry.HighRiskPermissions(manifest.Permissions, 6) {
			fmt.Fprintf(os.Stderr, "⚠ 高风险权限: %s\n", key)
		}
	}

	// 可选：解析 install.yaml
	var installSpec *kstypes.InstallSpec
	installPath := filepath.Join(projectDir, "install.yaml")
	if installData, err := os.ReadFile(installPath); err == nil {
		installSpec, err = kstypes.ParseInstallSpec(installData)
		if err != nil {
			return nil, fmt.Errorf("解析 install.yaml 失败: %w", err)
		}
		if err := installSpec.Validate(); err != nil {
			return nil, fmt.Errorf("install.yaml 校验失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 install.yaml 失败: %w", err)
	}

	// 2. 创建输出目录
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, err
	}

	// 3. 打包 tarball
	tarballName := fmt.Sprintf("%s-%s.tar.gz", manifest.ID, manifest.Version)
	tarballPath := filepath.Join(outputDir, tarballName)

	if err := createTarball(projectDir, tarballPath); err != nil {
		_ = os.Remove(tarballPath)
		return nil, fmt.Errorf("打包失败: %w", err)
	}

	// 4. 计算 checksum
	checksum, err := SHA256File(tarballPath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(tarballPath)
	if err != nil {
		return nil, fmt.Errorf("查询 tarball 信息失败: %w", err)
	}
	fmt.Printf("✓ 构建完成: %s (%d bytes, sha256:%s)\n", tarballName, info.Size(), checksum[:16]+"...")

	return &BuildResult{
		AppID:       manifest.ID,
		Version:     manifest.Version,
		TarballPath: tarballPath,
		TarballSize: info.Size(),
		Checksum:    checksum,
		AppSpec:     manifest,
		InstallSpec: installSpec,
	}, nil
}

// BuildWithOptions 是 Build 的扩展版本，支持 preflight 和 dry-run。
// projectDir 为输入项目目录；outputDir 为 tarball 输出目录（dry-run 时不创建）。
func BuildWithOptions(projectDir, outputDir string, opts *BuildOptions) (*BuildResult, *PreflightReport, error) {
	if opts == nil {
		opts = &BuildOptions{}
	}
	report, err := RunPreflight(projectDir, opts.Preflight)
	if err != nil {
		return nil, report, fmt.Errorf("preflight 失败: %w", err)
	}
	if !report.OK() {
		return nil, report, fmt.Errorf("preflight 未通过")
	}
	if opts.DryRun {
		// dry-run：跳过实际打包，仅返回 manifest 信息
		manifestPath := filepath.Join(projectDir, "manifest.yaml")
		manifestData, mErr := os.ReadFile(manifestPath)
		if mErr != nil {
			return nil, report, fmt.Errorf("读取 manifest.yaml 失败: %w", mErr)
		}
		manifest, mErr := kstypes.ParseAppSpec(manifestData)
		if mErr != nil {
			return nil, report, fmt.Errorf("解析 manifest 失败: %w", mErr)
		}
		return &BuildResult{
			AppID:   manifest.ID,
			Version: manifest.Version,
			AppSpec: manifest,
		}, report, nil
	}
	// 走现有 Build 流程（已通过 preflight，重复部分可接受）
	res, err := Build(projectDir, outputDir)
	if err != nil {
		return nil, report, err
	}
	return res, report, nil
}

// createTarball 将 srcDir 下的项目文件打包为 gzip tarball 到 dst。
// 会跳过以 "." 开头的文件/目录以及 dist/ 输出目录。
func createTarball(srcDir, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return relErr
		}

		// 关键修复：根目录 rel == "." 必须特殊处理，不要 skip
		if rel == "." {
			return nil
		}

		// 跳过隐藏文件（以 . 开头）和 dist/ 输出目录
		// 但不跳过单点 "."（根目录），这是 filepath.Walk 第一次回调
		parts := strings.Split(rel, string(filepath.Separator))
		if strings.HasPrefix(parts[0], ".") || parts[0] == "dist" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// MVP 范围：跳过符号链接，避免跨目录/循环链接风险
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
	})
}
