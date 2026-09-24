// verify 是 Compose 中的可观察验收服务：等待前后端就绪后，
// 通过前端 nginx 代理（端到端）与后端直连两条路径执行验收用例，
// 每个用例打印 [PASS]/[FAIL]，全部通过则以 0 退出。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	backendURL  = envOr("BACKEND_URL", "http://backend:8080")
	frontendURL = envOr("FRONTEND_URL", "http://frontend:80")

	passed, failed int
	client         = &http.Client{Timeout: 120 * time.Second}
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func check(name string, cond bool, detail string) {
	if cond {
		passed++
		fmt.Printf("[PASS] %s\n", name)
	} else {
		failed++
		fmt.Printf("[FAIL] %s: %s\n", name, detail)
	}
}

// ---------- 与后端一致的本地参照实现（暴力穷举，用于独立核对） ----------

func applyPose(p, r, c, n int) (int, int) {
	switch p {
	case 0:
		return r, c
	case 1:
		return c, n - 1 - r
	case 2:
		return n - 1 - r, n - 1 - c
	case 3:
		return n - 1 - c, r
	case 4:
		return r, n - 1 - c
	case 5:
		return n - 1 - r, c
	case 6:
		return c, r
	default:
		return n - 1 - c, n - 1 - r
	}
}

type grid [][]int

func countOverlap(ref, rec grid, n, p, dy, dx int) int {
	cnt := 0
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(p, r, c, n)
			tr, tc := pr+dy, pc+dx
			if tr >= 0 && tr < n && tc >= 0 && tc < n && ref[tr][tc] != 0 {
				cnt++
			}
		}
	}
	return cnt
}

type expected struct {
	max, ties, pose, dy, dx int
}

func bruteForce(ref, rec grid, n int) expected {
	e := expected{max: -1}
	for p := 0; p < 8; p++ {
		for dy := -(n - 1); dy <= n-1; dy++ {
			for dx := -(n - 1); dx <= n-1; dx++ {
				v := countOverlap(ref, rec, n, p, dy, dx)
				if v > e.max {
					e = expected{max: v, ties: 1, pose: p, dy: dy, dx: dx}
				} else if v == e.max {
					e.ties++
				}
			}
		}
	}
	return e
}

func newGrid(n int) grid {
	g := make(grid, n)
	for i := range g {
		g[i] = make([]int, n)
	}
	return g
}

func gridToText(g grid) string {
	var b strings.Builder
	for r, row := range g {
		for _, v := range row {
			if v != 0 {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		if r+1 < len(g) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func countOnes(g grid) int {
	cnt := 0
	for _, row := range g {
		for _, v := range row {
			if v != 0 {
				cnt++
			}
		}
	}
	return cnt
}

// ---------- API 结构 ----------

type apiErr struct {
	Field   string `json:"field"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

type auditResp struct {
	N              int `json:"n"`
	ReferenceCount int `json:"referenceCount"`
	RecheckCount   int `json:"recheckCount"`
	MaxOverlap     int `json:"maxOverlap"`
	TieCount       int `json:"tieCount"`
	Transform      struct {
		PoseIndex int    `json:"poseIndex"`
		Pose      string `json:"pose"`
		PoseLabel string `json:"poseLabel"`
		Dy        int    `json:"dy"`
		Dx        int    `json:"dx"`
	} `json:"transform"`
	Overlay struct {
		Matched       [][2]int `json:"matched"`
		ReferenceOnly [][2]int `json:"referenceOnly"`
		RecheckOnly   [][2]int `json:"recheckOnly"`
		RecheckOut    int      `json:"recheckOutOfCanvas"`
	} `json:"overlay"`
	ElapsedMs int64 `json:"elapsedMs"`
}

func postAudit(base, refText, recText string) (int, []byte, error) {
	body, _ := json.Marshal(map[string]string{"reference": refText, "recheck": recText})
	resp, err := client.Post(base+"/api/audit", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

func waitFor(name, url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Printf("[INFO] %s 就绪：%s\n", name, url)
				return true
			}
		}
		time.Sleep(time.Second)
	}
	return false
}

// ---------- 验收用例 ----------

// cluster 是 16x16 内的一组非对称点，作为确定性用例基础。
var cluster = [][2]int{
	{2, 3}, {3, 9}, {4, 4}, {6, 12}, {7, 7}, {9, 11},
	{11, 5}, {12, 10}, {13, 13}, {5, 2}, {10, 8}, {8, 6},
}

func caseDeterministic(base string) {
	const n = 16
	const pose, dy, dx = 1, 2, -1
	ref := newGrid(n)
	rec := newGrid(n)
	for _, p := range cluster {
		ref[p[0]][p[1]] = 1
		pr, pc := applyPose(pose, p[0], p[1], n)
		rec[pr+dy][pc+dx] = 1
	}
	status, body, err := postAudit(base, gridToText(ref), gridToText(rec))
	if err != nil {
		check("确定性姿态+平移用例", false, err.Error())
		return
	}
	var resp auditResp
	if err := json.Unmarshal(body, &resp); err != nil || status != 200 {
		check("确定性姿态+平移用例", false, fmt.Sprintf("status=%d body=%s", status, body))
		return
	}
	e := bruteForce(ref, rec, n)
	ok := resp.N == n &&
		resp.ReferenceCount == len(cluster) && resp.RecheckCount == len(cluster) &&
		resp.MaxOverlap == e.max && resp.TieCount == e.ties &&
		resp.Transform.PoseIndex == e.pose && resp.Transform.Dy == e.dy && resp.Transform.Dx == e.dx
	check("确定性姿态+平移用例（与本地暴力穷举一致）", ok,
		fmt.Sprintf("api=%+v transform=%+v 期望=%+v", resp, resp.Transform, e))

	// 叠加证据一致性：matched/recheckOnly 必须正好构成复检图变换后的点集。
	want := map[[2]int]bool{}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] != 0 {
				pr, pc := applyPose(resp.Transform.PoseIndex, r, c, n)
				want[[2]int{pr + resp.Transform.Dy, pc + resp.Transform.Dx}] = true
			}
		}
	}
	gotSet := map[[2]int]bool{}
	dup := false
	for _, p := range resp.Overlay.Matched {
		if gotSet[p] {
			dup = true
		}
		gotSet[p] = true
	}
	for _, p := range resp.Overlay.RecheckOnly {
		if gotSet[p] {
			dup = true
		}
		gotSet[p] = true
	}
	same := len(want) == len(gotSet) && !dup
	for p := range want {
		if !gotSet[p] {
			same = false
		}
	}
	check("叠加证据与规范变换自洽", same && len(resp.Overlay.Matched) == resp.MaxOverlap &&
		len(resp.Overlay.ReferenceOnly) == resp.ReferenceCount-resp.MaxOverlap &&
		resp.Overlay.RecheckOut == 0,
		fmt.Sprintf("matched=%d recOnly=%d recOut=%d", len(resp.Overlay.Matched),
			len(resp.Overlay.RecheckOnly), resp.Overlay.RecheckOut))
}

func caseDenseAllOnes(base string, n int) {
	g := newGrid(n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			g[r][c] = 1
		}
	}
	text := gridToText(g)
	status, body, err := postAudit(base, text, text)
	if err != nil {
		check(fmt.Sprintf("稠密满尺寸 %dx%d 用例", n, n), false, err.Error())
		return
	}
	var resp auditResp
	if err := json.Unmarshal(body, &resp); err != nil || status != 200 {
		check(fmt.Sprintf("稠密满尺寸 %dx%d 用例", n, n), false, fmt.Sprintf("status=%d", status))
		return
	}
	ok := resp.MaxOverlap == n*n && resp.TieCount == 8 &&
		resp.Transform.PoseIndex == 0 && resp.Transform.Dy == 0 && resp.Transform.Dx == 0 &&
		resp.ReferenceCount == n*n && resp.RecheckCount == n*n
	check(fmt.Sprintf("稠密满尺寸 %dx%d 用例（耗时 %d ms）", n, n, resp.ElapsedMs), ok,
		fmt.Sprintf("max=%d ties=%d transform=%+v", resp.MaxOverlap, resp.TieCount, resp.Transform))
}

func caseMirror(base string) {
	const n = 16
	ref := newGrid(n)
	rec := newGrid(n)
	for _, p := range cluster {
		ref[p[0]][p[1]] = 1
		pr, pc := applyPose(4, p[0], p[1], n) // 左右镜像，无平移
		rec[pr][pc] = 1
	}
	status, body, err := postAudit(base, gridToText(ref), gridToText(rec))
	if err != nil {
		check("镜像姿态用例", false, err.Error())
		return
	}
	var resp auditResp
	if err := json.Unmarshal(body, &resp); err != nil || status != 200 {
		check("镜像姿态用例", false, fmt.Sprintf("status=%d", status))
		return
	}
	e := bruteForce(ref, rec, n)
	ok := resp.MaxOverlap == e.max && resp.MaxOverlap == len(cluster) &&
		resp.TieCount == e.ties && resp.Transform.PoseIndex == e.pose &&
		resp.Transform.Dy == e.dy && resp.Transform.Dx == e.dx
	check("镜像姿态用例（与本地暴力穷举一致）", ok,
		fmt.Sprintf("transform=%+v 期望=%+v", resp.Transform, e))
}

func caseOutOfCanvas(base string) {
	const n = 16
	ref := newGrid(n)
	rec := newGrid(n)
	for _, p := range cluster {
		rec[p[0]][p[1]] = 1
		ref[p[0]+2][p[1]+1] = 1
	}
	// 边缘噪声点：规范平移 (+2,+1) 后 (15,15)->(17,16) 移出画布，(0,0)->(2,1) 落在画布内但不重合。
	rec[15][15] = 1
	rec[0][0] = 1
	status, body, err := postAudit(base, gridToText(ref), gridToText(rec))
	if err != nil {
		check("移出画布计数用例", false, err.Error())
		return
	}
	var resp auditResp
	if err := json.Unmarshal(body, &resp); err != nil || status != 200 {
		check("移出画布计数用例", false, fmt.Sprintf("status=%d", status))
		return
	}
	e := bruteForce(ref, rec, n)
	// 独立复算规范变换下移出画布的点数
	recOut := 0
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(e.pose, r, c, n)
			tr, tc := pr+e.dy, pc+e.dx
			if tr < 0 || tr >= n || tc < 0 || tc >= n {
				recOut++
			}
		}
	}
	ok := resp.MaxOverlap == e.max && resp.TieCount == e.ties &&
		resp.Transform.PoseIndex == e.pose && resp.Transform.Dy == e.dy && resp.Transform.Dx == e.dx &&
		resp.RecheckCount == countOnes(rec) && resp.Overlay.RecheckOut == recOut && recOut > 0
	check("移出画布缺陷仍计入总数用例", ok,
		fmt.Sprintf("max=%d/%d recheckCount=%d recOut=%d/%d transform=%+v",
			resp.MaxOverlap, e.max, resp.RecheckCount, resp.Overlay.RecheckOut, recOut, resp.Transform))
}

func caseValidation(base string) {
	valid := gridToText(newGrid(16))

	// 非法字符：第 3 行第 5 列
	badChar := newGrid(16)
	lines := strings.Split(gridToText(badChar), "\n")
	row := []byte(lines[2])
	row[4] = 'x'
	lines[2] = string(row)
	status, body, _ := postAudit(base, valid, strings.Join(lines, "\n"))
	var e1 struct {
		Error apiErr `json:"error"`
	}
	json.Unmarshal(body, &e1)
	check("非法字符定位到输入/行/列", status == 400 && e1.Error.Field == "recheck" &&
		e1.Error.Line == 3 && e1.Error.Column == 5,
		fmt.Sprintf("status=%d body=%s", status, body))

	// 行宽不一致：参考图第 7 行少 1 字符
	lines = strings.Split(valid, "\n")
	lines[6] = lines[6][:15]
	status, body, _ = postAudit(base, strings.Join(lines, "\n"), valid)
	var e2 struct {
		Error apiErr `json:"error"`
	}
	json.Unmarshal(body, &e2)
	check("行宽不一致定位到输入/行", status == 400 && e2.Error.Field == "reference" && e2.Error.Line == 7,
		fmt.Sprintf("status=%d body=%s", status, body))

	// 非方阵：16 行 × 15 列
	notSquare := make([]string, 16)
	for i := range notSquare {
		notSquare[i] = strings.Repeat("0", 15)
	}
	status, body, _ = postAudit(base, strings.Join(notSquare, "\n"), valid)
	var e3 struct {
		Error apiErr `json:"error"`
	}
	json.Unmarshal(body, &e3)
	check("非方阵被拒绝并定位输入", status == 400 && e3.Error.Field == "reference" &&
		strings.Contains(e3.Error.Message, "不是方阵"),
		fmt.Sprintf("status=%d body=%s", status, body))

	// 边长越界：8x8
	small := gridToText(newGrid(8))
	status, body, _ = postAudit(base, small, small)
	var e4 struct {
		Error apiErr `json:"error"`
	}
	json.Unmarshal(body, &e4)
	check("边长越界被拒绝并定位输入", status == 400 && e4.Error.Field == "reference" &&
		strings.Contains(e4.Error.Message, "超出允许范围"),
		fmt.Sprintf("status=%d body=%s", status, body))

	// 两图边长不一致：16 vs 32
	status, body, _ = postAudit(base, valid, gridToText(newGrid(32)))
	var e5 struct {
		Error apiErr `json:"error"`
	}
	json.Unmarshal(body, &e5)
	check("两图边长不一致被拒绝", status == 400 && e5.Error.Field == "recheck" &&
		strings.Contains(e5.Error.Message, "边长不一致"),
		fmt.Sprintf("status=%d body=%s", status, body))

	// 空输入
	status, body, _ = postAudit(base, valid, "  \n ")
	var e6 struct {
		Error apiErr `json:"error"`
	}
	json.Unmarshal(body, &e6)
	check("空输入被拒绝并定位输入", status == 400 && e6.Error.Field == "recheck",
		fmt.Sprintf("status=%d body=%s", status, body))
}

func caseIdentityDirect(base string) {
	const n = 16
	ref := newGrid(n)
	for _, p := range cluster {
		ref[p[0]][p[1]] = 1
	}
	text := gridToText(ref)
	status, body, err := postAudit(base, text, text)
	if err != nil {
		check("后端直连恒等用例", false, err.Error())
		return
	}
	var resp auditResp
	if err := json.Unmarshal(body, &resp); err != nil || status != 200 {
		check("后端直连恒等用例", false, fmt.Sprintf("status=%d", status))
		return
	}
	ok := resp.MaxOverlap == len(cluster) && resp.Transform.PoseIndex == 0 &&
		resp.Transform.Dy == 0 && resp.Transform.Dx == 0 && resp.TieCount >= 1
	check("后端直连恒等用例", ok, fmt.Sprintf("resp=%+v", resp))
}

func main() {
	fmt.Printf("[INFO] verify 启动，BACKEND_URL=%s FRONTEND_URL=%s\n", backendURL, frontendURL)

	check("后端健康检查 /api/health", waitFor("backend", backendURL+"/api/health", 90*time.Second), "超时未就绪")
	check("前端首页可访问", waitFor("frontend", frontendURL+"/", 90*time.Second), "超时未就绪")

	resp, err := client.Get(frontendURL + "/")
	body := ""
	if err == nil {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		body = string(data)
	}
	check("前端页面挂载点存在", err == nil && resp.StatusCode == 200 && strings.Contains(body, `id="root"`),
		"首页缺少 #root")

	// 端到端：经前端 nginx 代理调用真实 Gin API
	caseDeterministic(frontendURL)
	caseMirror(frontendURL)
	caseOutOfCanvas(frontendURL)
	caseDenseAllOnes(frontendURL, 64)
	caseDenseAllOnes(frontendURL, 512)
	caseValidation(frontendURL)

	// 直连后端，排除代理因素
	caseIdentityDirect(backendURL)

	fmt.Printf("[SUMMARY] verify 完成：%d 通过，%d 失败\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
