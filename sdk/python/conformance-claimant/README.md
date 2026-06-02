# ks_app Conformance Claimant (Python)

**Claims compliance with: conformance-v1.0.0**

## 这是什么

ks_app Python SDK（`ks-devkit/sdk/python/src/ks_app/`）
对 `../../conformance/auth/` 定义的契约的最小声明实现。

## 本地运行

```bash
# 1. 安装（editable）
cd ks-devkit/sdk/python/conformance-claimant
uv pip install -e .

# 2. 跑 conformance
cd ks-devkit/conformance/auth
./orchestrate.sh \
    --claimant-cmd="cd ../../sdk/python/conformance-claimant && python main.py" \
    --claimant-port=8080
```

## 契约守护的不变量

（与 Go claimant 相同，见 `../../sdk/go/conformance-claimant/README.md`）

## 和 templates/service-python 的区别

`templates/service-python/` 会跟着"开发体验"改进。本目录是 conformance
测试的稳定被测对象，**行为不可变**。
