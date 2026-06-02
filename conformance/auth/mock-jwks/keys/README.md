# Runtime TEST-ONLY RSA Keys

本目录只保留说明文件。conformance 测试需要的 RSA key pair 由
`../generate.sh` 在运行时写入临时目录，仓库不再跟踪 PEM 私钥 fixture。

生成出的 RSA 密钥对仅用于本地 conformance 测试，不得用于任何真实环境。

手动调试时请使用：

```bash
tmpdir="$(mktemp -d)"
../generate.sh "$tmpdir"
../serve.sh 9999 "$tmpdir"
```
