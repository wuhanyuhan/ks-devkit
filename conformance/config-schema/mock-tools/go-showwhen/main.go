// Package main — ks-conf-go-showwhen：show_when DSL 编译 mock-tool。
//
// 用法：
//
//	echo "backend == 'github'" | ks-conf-go-showwhen <field_name>
//
// 输入：stdin 是 DSL 源码（可能含换行，末尾会 trim）。
// 输出（stdout）：编译后的 ifThenElse 对象，canonical JSON 序列化
// （字段按字典序排序，无缩进，无尾随空格）。
//
// 错误退出码：
//   - 0：编译成功
//   - 10：parse error（spec 允许的运行期错误，如 arithmetic / cross-level）
//   - 11：SyntaxError（programmer error，如括号嵌套 — Go 侧是 panic，本 CLI recover）
//   - 2：其他用法错
//
// 字段名由 argv[1] 传入，与 testvectors.json 的 context.field_under_if 对齐。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/ksconfig"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: echo '<dsl>' | ks-conf-go-showwhen <field_name>")
		os.Exit(2)
	}
	fieldName := os.Args[1]

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读 stdin 失败：%v\n", err)
		os.Exit(2)
	}
	dsl := strings.TrimRight(string(raw), "\r\n\t ")

	// recover 括号嵌套 panic，映射为退出码 11。
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "SyntaxError: %v\n", r)
			os.Exit(11)
		}
	}()

	ifThenElse, _, err := ksconfig.CompileShowWhen(dsl, fieldName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(10)
	}

	// canonical JSON：sort_keys + compact + no escape html
	out, err := canonicalMarshal(ifThenElse)
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON 序列化失败：%v\n", err)
		os.Exit(2)
	}
	fmt.Print(string(out))
}

// canonicalMarshal 把 map/slice/primitive 结构按字典序排键、compact 序列化。
// Go 标准库的 json.Marshal 默认按 map key 字典序输出，所以主要是把 compact +
// HTMLEscape=false + float/int 稳定化做好。我们的 show_when compile 产物里
// 数字字面量是 int64，不会出现浮点；bool 直接 true/false；string 用 UTF-8。
func canonicalMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// encoder 会输出末尾 '\n'，encode 前对 map 全部递归规整
	if err := enc.Encode(normalize(v)); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	return bytes.TrimRight(b, "\n"), nil
}

// normalize 递归规整 value，保证 slice 内嵌的 map 也被 encoder 看到。
//
// Go encoder 已经对 map[string]any 按 key 字典序输出，
// 这里不需要显式 sort.Strings(keys) — Go map 本身无序，构造有序 map 再遍历
// 是死循环式冗余。删掉 sort 逻辑保留递归结构（因为我们需要递归到 slice 里的
// 嵌套 map 做 normalize；如果什么都不做，encoder 对 map 自身 sort，但 slice 里
// 的嵌套 map 会被 encoder 看到并 sort — 所以其实 encoder 会全部负责，normalize
// 在目前 show_when 输出结构下是纯 no-op。保留函数轮廓只为调试、可读。）
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = normalize(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = normalize(it)
		}
		return out
	default:
		return x
	}
}
