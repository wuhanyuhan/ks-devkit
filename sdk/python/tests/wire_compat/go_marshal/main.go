// 读 stdin 一行 JSON `{"widget":"...", "data": {...}}` →
// 用 ksapp.NewToolResult().WithUIData(decoded) 序列化 → 输出到 stdout。
//
// 仅 5 个 widget data 类型支持；按 widget URI 分流。
//
// 用途：sdk/python/tests/test_wire_compat.py 通过 subprocess 调本程序，
// 校验 Python ↔ Go 两端 widget data schema 序列化的字段集合一致性。
// 任何 wire 不一致都会被本程序 + Python 测试联合捕获。
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp"
	kstypes "github.com/wuhanyuhan/ks-types"
)

type input struct {
	Widget string          `json:"widget"`
	Data   json.RawMessage `json:"data"`
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var widgetData any
	switch in.Widget {
	case "ks://widgets/list-actions@v1":
		var d kstypes.WidgetListActionsV1
		if err := json.Unmarshal(in.Data, &d); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		widgetData = d
	case "ks://widgets/diff-review@v1":
		var d kstypes.WidgetDiffReviewV1
		if err := json.Unmarshal(in.Data, &d); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		widgetData = d
	case "ks://widgets/timeline@v1":
		var d kstypes.WidgetTimelineV1
		if err := json.Unmarshal(in.Data, &d); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		widgetData = d
	case "ks://widgets/card-grid@v1":
		var d kstypes.WidgetCardGridV1
		if err := json.Unmarshal(in.Data, &d); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		widgetData = d
	case "ks://widgets/image-variants@v1":
		var d kstypes.WidgetImageVariantsV1
		if err := json.Unmarshal(in.Data, &d); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		widgetData = d
	default:
		fmt.Fprintf(os.Stderr, "unsupported widget: %s\n", in.Widget)
		os.Exit(2)
	}

	out, err := ksapp.NewToolResult().WithUIData(widgetData).MarshalJSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
