package main

import (
	"fmt"
	"strings"
)

const (
	minN = 16
	maxN = 512
)

// InputError 把校验失败定位到具体输入（field）、行（line，1 起）与列（column，1 起）。
type InputError struct {
	Field   string `json:"field,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
}

func (e *InputError) Error() string {
	loc := e.Field
	if loc == "" {
		loc = "request"
	}
	if e.Line > 0 {
		loc += fmt.Sprintf(" 第%d行", e.Line)
	}
	if e.Column > 0 {
		loc += fmt.Sprintf(" 第%d列", e.Column)
	}
	return loc + ": " + e.Message
}

// parseMatrix 把一段文本解析为二值方阵。
// 每行允许首尾空白与 CRLF；行内只允许字符 '0' 与 '1'；
// 行数必须等于行宽（方阵），且边长须在 [minN, maxN] 内。
// 任何非法尺寸、非法字符或行宽不一致都会带上具体位置返回 *InputError。
func parseMatrix(field, text string) ([][]uint8, int, *InputError) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, 0, &InputError{Field: field, Message: "内容为空，需要 0/1 方阵文本"}
	}
	lines := strings.Split(trimmed, "\n")
	rows := len(lines)
	width := -1
	matrix := make([][]uint8, rows)
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if width < 0 {
			width = len(line)
		} else if len(line) != width {
			return nil, 0, &InputError{
				Field:   field,
				Line:    i + 1,
				Message: fmt.Sprintf("行宽不一致：本行 %d 字符，首行 %d 字符", len(line), width),
			}
		}
		row := make([]uint8, len(line))
		for j := 0; j < len(line); j++ {
			switch line[j] {
			case '0':
				// 保持 0
			case '1':
				row[j] = 1
			default:
				return nil, 0, &InputError{
					Field:   field,
					Line:    i + 1,
					Column:  j + 1,
					Message: fmt.Sprintf("非法字符 %q，仅允许 0 和 1", line[j]),
				}
			}
		}
		matrix[i] = row
	}
	if width != rows {
		return nil, 0, &InputError{
			Field:   field,
			Message: fmt.Sprintf("不是方阵：共 %d 行、每行 %d 字符，要求行数等于行宽", rows, width),
		}
	}
	if rows < minN || rows > maxN {
		return nil, 0, &InputError{
			Field:   field,
			Message: fmt.Sprintf("边长 %d 超出允许范围 [%d, %d]", rows, minN, maxN),
		}
	}
	return matrix, rows, nil
}
