package manifest

import (
	"fmt"
	"io"
	"strings"
)

// 多行字段终止哨兵：用户在单独一行输入 "::end" 表示输入结束。
// 选哨兵而非空行：作者写 markdown changelog 时常会有空行（### Added 与列表之间），
// 用空行终止会误伤。
const multilineEndSentinel = "::end"

// fieldLabel 把 manifest 字段名映射为面向用户的中文提示。
// 未识别的 field 直接用字段名兜底，便于未来扩展无需同步改这里。
func fieldLabel(field string) string {
	switch field {
	case "summary":
		return "请补充一句话 summary（< 30 字，留空跳过）"
	case "tags":
		return "请补充 tags（逗号分隔，3-6 个，留空跳过）"
	case "store.audience":
		return "请补充 store.audience（目标用户，逗号分隔，留空跳过）"
	case "store.highlights":
		return "请补充 store.highlights（展示亮点，逗号分隔，留空跳过）"
	case "store.try_prompts":
		return "请补充 store.try_prompts（示例提问，逗号分隔，留空跳过）"
	case "category":
		return "请选择 category（开发流程/办公自动化/AI 工具/数据处理/其他，留空跳过）"
	case "changelog":
		return fmt.Sprintf("请补充本版本 changelog（markdown，留空跳过；输入 %s 结束多行）", multilineEndSentinel)
	case "description":
		return fmt.Sprintf("请补充 description（markdown，留空跳过；输入 %s 结束多行）", multilineEndSentinel)
	default:
		return field
	}
}

// PromptForField 是 manifest fallback chain 第 3 级的核心：从 in 读用户输入，
// 把提示语写到 out，返回用户输入（或 defaultVal）。
//
// 单行字段（summary / tags / category / 未知字段）：读一行去首尾空白返回。
// 多行字段（changelog / description）：循环读直到读到独立一行 "::end" 或 EOF。
//
// 空输入语义：若用户直接回车（且无 default），返回 ""，调用方可解读为"作者主动跳过"。
// 若有 defaultVal，空输入返回 defaultVal（典型用法：作者编辑 LLM 建议时直接回车保留建议值）。
//
// 设计取舍：纯 io.Reader/Writer 接口、逐字节读避免 bufio 预读 → 调用方可串联
// 多次 PromptForField 复用同一 io.Reader（fallback chain 多字段引导场景必需）。
// 不依赖 survey/v2 terminal 检测路径，CI / 重定向 stdin 行为可预测。
func PromptForField(in io.Reader, out io.Writer, field, defaultVal string) (string, error) {
	label := fieldLabel(field)
	if defaultVal != "" {
		fmt.Fprintf(out, "%s\n[默认: %s]\n> ", label, defaultVal)
	} else {
		fmt.Fprintf(out, "%s\n> ", label)
	}

	if field == "changelog" || field == "description" {
		return readMultiline(in, defaultVal)
	}
	return readSingleLine(in, defaultVal)
}

// readLineNoBuffer 从 r 逐字节读一行直到 \n 或 EOF，不使用 bufio 避免预读后续字节。
// 返回的 line 不含末尾 \n。EOF 不视为错误（返回已积累的 line 及空 error）。
//
// 性能注意：每字节一次 Read 调用；CLI 交互（人键入间隔毫秒级）和单元测试
// （strings.NewReader 内存读）场景都不是瓶颈。如未来批量场景需要，可加 bufio
// 但要确保整个会话只 wrap 一次。
func readLineNoBuffer(r io.Reader) (string, error) {
	var buf []byte
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				return string(buf), nil
			}
			buf = append(buf, one[0])
		}
		if err != nil {
			if err == io.EOF {
				return string(buf), nil
			}
			return string(buf), err
		}
	}
}

// readSingleLine 读一行直到 \n 或 EOF。
// 末尾换行被剥离；首尾空白 trim；空输入回 defaultVal。
func readSingleLine(r io.Reader, defaultVal string) (string, error) {
	line, err := readLineNoBuffer(r)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(line)
	if v == "" {
		return defaultVal, nil
	}
	return v, nil
}

// readMultiline 读多行直到独立的 "::end" 哨兵行或 EOF（且当前行为空）。
// 每行末尾 \n 剥离但行内空白保留（markdown 缩进语义）。
// 整体输入若 trim 后为空，返回 defaultVal。
func readMultiline(r io.Reader, defaultVal string) (string, error) {
	var lines []string
	for {
		line, err := readLineNoBuffer(r)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(line) == multilineEndSentinel {
			break
		}
		// EOF 且本行为空：终止收集
		// readLineNoBuffer 对 EOF 不返错，靠"未拿到内容 + 下一次还是空"判终止
		// 这里检测：行空 + 到达 EOF（再读一次确认）
		if line == "" {
			// 探测是否真 EOF：读一字节，若 EOF 则退出，否则说明是用户输了空白行 → 收一个空行
			one := make([]byte, 1)
			n, perr := r.Read(one)
			if n == 0 && perr == io.EOF {
				break
			}
			if perr != nil && perr != io.EOF {
				return "", perr
			}
			// 把刚读的字节算到下一行的开头
			if one[0] == '\n' {
				lines = append(lines, "")
				continue
			}
			// 非 \n：把这字节当作下一行第一个字符再读一行
			rest, err := readLineNoBuffer(r)
			if err != nil {
				return "", err
			}
			lines = append(lines, string(one)+rest)
			continue
		}
		lines = append(lines, line)
	}
	v := strings.TrimSpace(strings.Join(lines, "\n"))
	if v == "" {
		return defaultVal, nil
	}
	return v, nil
}
