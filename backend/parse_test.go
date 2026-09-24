package main

import (
	"strings"
	"testing"
)

func makeText(n int, fill func(r, c int) byte) string {
	var b strings.Builder
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			b.WriteByte(fill(r, c))
		}
		if r+1 < n {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func TestParseValid(t *testing.T) {
	m, n, err := parseMatrix("reference", makeText(16, func(r, c int) byte {
		if (r+c)%3 == 0 {
			return '1'
		}
		return '0'
	}))
	if err != nil {
		t.Fatal(err)
	}
	if n != 16 {
		t.Fatalf("边长应为 16，得到 %d", n)
	}
	if m[0][0] != 1 || m[0][1] != 0 {
		t.Fatal("矩阵内容解析错误")
	}
}

func TestParseCRLFAndSpaces(t *testing.T) {
	text := strings.ReplaceAll(makeText(16, func(r, c int) byte { return '0' }), "\n", "\r\n")
	text = "  " + text + "  \n"
	_, n, err := parseMatrix("reference", text)
	if err != nil {
		t.Fatal(err)
	}
	if n != 16 {
		t.Fatalf("边长应为 16，得到 %d", n)
	}
}

func TestParseBadCharLocated(t *testing.T) {
	text := makeText(16, func(r, c int) byte {
		if r == 4 && c == 6 {
			return 'x'
		}
		return '0'
	})
	_, _, err := parseMatrix("recheck", text)
	if err == nil {
		t.Fatal("应报非法字符")
	}
	if err.Field != "recheck" || err.Line != 5 || err.Column != 7 {
		t.Fatalf("非法字符定位错误：%+v", err)
	}
}

func TestParseWidthMismatchLocated(t *testing.T) {
	lines := make([]string, 16)
	for i := range lines {
		lines[i] = strings.Repeat("0", 16)
	}
	lines[9] = strings.Repeat("0", 15)
	_, _, err := parseMatrix("reference", strings.Join(lines, "\n"))
	if err == nil {
		t.Fatal("应报行宽不一致")
	}
	if err.Field != "reference" || err.Line != 10 {
		t.Fatalf("行宽错误定位错误：%+v", err)
	}
}

func TestParseNotSquare(t *testing.T) {
	lines := make([]string, 16)
	for i := range lines {
		lines[i] = strings.Repeat("0", 20)
	}
	_, _, err := parseMatrix("reference", strings.Join(lines, "\n"))
	if err == nil || !strings.Contains(err.Message, "不是方阵") {
		t.Fatalf("应报非方阵：%v", err)
	}
}

func TestParseSizeRange(t *testing.T) {
	_, _, err := parseMatrix("reference", makeText(8, func(r, c int) byte { return '0' }))
	if err == nil || !strings.Contains(err.Message, "超出允许范围") {
		t.Fatalf("应报边长越界：%v", err)
	}
	_, _, err = parseMatrix("reference", makeText(513, func(r, c int) byte { return '0' }))
	if err == nil || !strings.Contains(err.Message, "超出允许范围") {
		t.Fatalf("应报边长越界：%v", err)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, _, err := parseMatrix("reference", "  \n "); err == nil {
		t.Fatal("空内容应报错")
	}
}
