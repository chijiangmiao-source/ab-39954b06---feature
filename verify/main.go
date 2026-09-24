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
	"regexp"
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

// ---------- 最小遮挡反证复核结构 ----------

type counterResp struct {
	N         int  `json:"n"`
	Feasible  bool `json:"feasible"`
	Canonical struct {
		PoseIndex int    `json:"poseIndex"`
		Pose      string `json:"pose"`
		PoseLabel string `json:"poseLabel"`
		Dy        int    `json:"dy"`
		Dx        int    `json:"dx"`
		Overlap   int    `json:"overlap"`
	} `json:"canonical"`
	Target struct {
		PoseIndex int    `json:"poseIndex"`
		Pose      string `json:"pose"`
		PoseLabel string `json:"poseLabel"`
		Dy        int    `json:"dy"`
		Dx        int    `json:"dx"`
		Overlap   int    `json:"overlap"`
	} `json:"target"`
	Gap    int `json:"gap"`
	Window *struct {
		Top              int      `json:"top"`
		Left             int      `json:"left"`
		Bottom           int      `json:"bottom"`
		Right            int      `json:"right"`
		Width            int      `json:"width"`
		Height           int      `json:"height"`
		Area             int      `json:"area"`
		RemovedCanonical int      `json:"removedCanonical"`
		RemovedTarget    int      `json:"removedTarget"`
		RemovedPoints    [][2]int `json:"removedPoints"`
		CanonicalAfter   int      `json:"canonicalOverlapAfter"`
		TargetAfter      int      `json:"targetOverlapAfter"`
	} `json:"window"`
}

func postCounter(base, refText, recText string, pose, dy, dx int) (int, []byte, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"reference": refText, "recheck": recText,
		"targetPose": pose, "targetDy": dy, "targetDx": dx,
	})
	resp, err := client.Post(base+"/api/counter-evidence", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

// hitSet 返回某候选（姿态 p、平移 dy,dx）命中的参考缺陷坐标集合。
func hitSet(ref, rec grid, n, p, dy, dx int) map[[2]int]bool {
	hit := map[[2]int]bool{}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(p, r, c, n)
			tr, tc := pr+dy, pc+dx
			if tr >= 0 && tr < n && tc >= 0 && tc < n && ref[tr][tc] != 0 {
				hit[[2]int{tr, tc}] = true
			}
		}
	}
	return hit
}

// bruteMinWindow 独立暴力参照：枚举全部轴对齐非空矩形，
// 找权值和 >= threshold 的最小面积窗口，同面积按 (上,左,下,右) 裁决。
func bruteMinWindow(weights []int, n, threshold int) ([4]int, bool) {
	best, found := [4]int{}, false
	bestArea := 0
	for t := 0; t < n; t++ {
		for b := t; b < n; b++ {
			for l := 0; l < n; l++ {
				sum := 0
				for r := l; r < n; r++ {
					for i := t; i <= b; i++ {
						sum += weights[i*n+r]
					}
					if sum >= threshold {
						cand := [4]int{t, l, b, r}
						area := (b - t + 1) * (r - l + 1)
						earlier := func() bool {
							for k := 0; k < 4; k++ {
								if cand[k] != best[k] {
									return cand[k] < best[k]
								}
							}
							return false
						}
						if !found || area < bestArea || (area == bestArea && earlier()) {
							found, bestArea, best = true, area, cand
						}
					}
				}
			}
		}
	}
	return best, found
}

// buildCounterWeights 按定义重建每个参考缺陷的命中差 (-1/0/1) 网格。
func buildCounterWeights(ref, rec grid, n, cp, cdy, cdx, tp, tdy, tdx int) ([]int, map[[2]int]bool, map[[2]int]bool) {
	cHit := hitSet(ref, rec, n, cp, cdy, cdx)
	tHit := hitSet(ref, rec, n, tp, tdy, tdx)
	w := make([]int, n*n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if ref[r][c] == 0 {
				continue
			}
			k := [2]int{r, c}
			v := 0
			if cHit[k] {
				v++
			}
			if tHit[k] {
				v--
			}
			w[r*n+c] = v
		}
	}
	return w, cHit, tHit
}

// counterFixtures 与后端单测构造一致：
// 共享命中 3 点（权 0）、规范独占 3 点（权 +1）、目标独占 2 点（权 -1）。
func counterFixtures() (grid, grid) {
	const n = 16
	ref, rec := newGrid(n), newGrid(n)
	for _, p := range [][2]int{{3, 3}, {3, 7}, {3, 11}} {
		ref[p[0]][p[1]] = 1
		rec[p[0]][p[1]] = 1
		rec[p[0]-1][p[1]] = 1
	}
	for _, p := range [][2]int{{10, 4}, {10, 5}, {10, 6}} {
		ref[p[0]][p[1]] = 1
		rec[p[0]-1][p[1]] = 1
	}
	for _, p := range [][2]int{{14, 1}, {14, 15}} {
		ref[p[0]][p[1]] = 1
		rec[p[0]][p[1]] = 1
	}
	return ref, rec
}

func caseCounterFalsifiable(base string) {
	const n = 16
	ref, rec := counterFixtures()
	// 经前端 nginx 代理从真实页面同源入口提交（目标候选 identity/(0,0)）。
	status, body, err := postCounter(base, gridToText(ref), gridToText(rec), 0, 0, 0)
	if err != nil {
		check("反证复核·可反证案例", false, err.Error())
		return
	}
	var resp counterResp
	if err := json.Unmarshal(body, &resp); err != nil || status != 200 {
		check("反证复核·可反证案例", false, fmt.Sprintf("status=%d body=%s", status, body))
		return
	}
	// 规范候选必须由服务端重算，且与本地暴力穷举一致。
	e := bruteForce(ref, rec, n)
	canOK := resp.Canonical.PoseIndex == e.pose && resp.Canonical.Dy == e.dy &&
		resp.Canonical.Dx == e.dx && resp.Canonical.Overlap == e.max && e.max == 6
	check("反证复核·服务端重新配准规范候选(identity,dy=1,6 命中)", canOK,
		fmt.Sprintf("canonical=%+v want=%+v", resp.Canonical, e))
	check("反证复核·目标候选 5 命中、gap=1",
		resp.Target.PoseIndex == 0 && resp.Target.Dy == 0 && resp.Target.Dx == 0 &&
			resp.Target.Overlap == 5 && resp.Gap == 1,
		fmt.Sprintf("target=%+v gap=%d", resp.Target, resp.Gap))

	// 独立按定义重建权值网格并暴力枚举最小窗口。
	weights, cHit, tHit := buildCounterWeights(ref, rec, n,
		e.pose, e.dy, e.dx, 0, 0, 0)
	wantWin, found := bruteMinWindow(weights, n, resp.Gap+1)
	ok := resp.Feasible && found && resp.Window != nil
	if ok {
		got := [4]int{resp.Window.Top, resp.Window.Left, resp.Window.Bottom, resp.Window.Right}
		ok = got == wantWin && wantWin == [4]int{10, 4, 10, 5} &&
			resp.Window.Area == 2 &&
			resp.Window.CanonicalAfter == 4 && resp.Window.TargetAfter == 5 &&
			resp.Window.TargetAfter > resp.Window.CanonicalAfter
		// 窗内移除点数与命中分类独立核对。
		remC, remT := 0, 0
		for _, p := range resp.Window.RemovedPoints {
			if cHit[p] {
				remC++
			}
			if tHit[p] {
				remT++
			}
		}
		ok = ok && len(resp.Window.RemovedPoints) == 2 &&
			remC == resp.Window.RemovedCanonical && remC == 2 &&
			remT == resp.Window.RemovedTarget && remT == 0
		check("反证复核·可反证案例（最小窗口 1×2@(10,4)，移除后 5>4）", ok,
			fmt.Sprintf("got=%v want=%v win=%+v", got, wantWin, resp.Window))
	} else {
		check("反证复核·可反证案例（最小窗口 1×2@(10,4)，移除后 5>4）", false,
			fmt.Sprintf("feasible=%v found=%v window=%+v", resp.Feasible, found, resp.Window))
	}
}

func caseCounterNonFalsifiable(base string) {
	const n = 16
	ref, rec := newGrid(n), newGrid(n)
	pts := [][2]int{
		{2, 3}, {3, 9}, {4, 4}, {5, 12}, {6, 7}, {7, 11},
		{8, 5}, {9, 10}, {10, 13}, {11, 2}, {12, 8}, {13, 6},
	}
	for _, p := range pts {
		ref[p[0]][p[1]] = 1
		rec[p[0]-1][p[1]] = 1 // 仅规范候选 dy=+1 命中
	}
	status, body, err := postCounter(base, gridToText(ref), gridToText(rec), 0, 0, 0)
	if err != nil {
		check("反证复核·不可反证案例", false, err.Error())
		return
	}
	var resp counterResp
	if err := json.Unmarshal(body, &resp); err != nil || status != 200 {
		check("反证复核·不可反证案例", false, fmt.Sprintf("status=%d body=%s", status, body))
		return
	}
	e := bruteForce(ref, rec, n)
	weights, _, _ := buildCounterWeights(ref, rec, n, e.pose, e.dy, e.dx, 0, 0, 0)
	_, found := bruteMinWindow(weights, n, resp.Gap+1)
	check("反证复核·不可反证案例（无窗口且不伪造矩形，规范 12:0）",
		!resp.Feasible && resp.Window == nil && !found &&
			resp.Canonical.Overlap == 12 && resp.Target.Overlap == 0 && resp.Gap == 12,
		fmt.Sprintf("feasible=%v win=%+v found=%v", resp.Feasible, resp.Window, found))
}

func caseCounterValidation(base string) {
	const n = 16
	ref, rec := counterFixtures()
	refText, recText := gridToText(ref), gridToText(rec)
	decodeErr := func(body []byte) apiErr {
		var v struct {
			E apiErr `json:"error"`
		}
		json.Unmarshal(body, &v)
		return v.E
	}

	status, body, _ := postCounter(base, refText, recText, 8, 0, 0)
	e1 := decodeErr(body)
	check("反证复核·姿态越界 400 并定位字段", status == 400 && e1.Field == "targetPose",
		fmt.Sprintf("status=%d body=%s", status, body))

	status, body, _ = postCounter(base, refText, recText, 0, n, 0)
	e2 := decodeErr(body)
	check("反证复核·纵移越界 400 并定位字段", status == 400 && e2.Field == "targetDy",
		fmt.Sprintf("status=%d body=%s", status, body))

	status, body, _ = postCounter(base, refText, recText, 0, 0, -n)
	e3 := decodeErr(body)
	check("反证复核·横移越界 400 并定位字段", status == 400 && e3.Field == "targetDx",
		fmt.Sprintf("status=%d body=%s", status, body))

	// 边长 97：反证入口拒绝（仅 16..96）。
	big := gridToText(newGrid(97))
	status, body, _ = postCounter(base, big, big, 0, 0, 0)
	e4 := decodeErr(body)
	check("反证复核·边长 97 被拒绝(仅支持 16..96)", status == 400 && e4.Field == "reference",
		fmt.Sprintf("status=%d body=%s", status, body))

	// 目标候选与规范候选相同。
	status, body, _ = postCounter(base, refText, recText, 0, 1, 0)
	e5 := decodeErr(body)
	check("反证复核·目标候选同规范候选被拒绝", status == 400 && e5.Field == "targetPose",
		fmt.Sprintf("status=%d body=%s", status, body))

	// 原图非法字符同样重新解析并定位行列。
	lines := strings.Split(recText, "\n")
	row := []byte(lines[1])
	row[2] = 'x'
	lines[1] = string(row)
	status, body, _ = postCounter(base, refText, strings.Join(lines, "\n"), 0, 0, 0)
	e6 := decodeErr(body)
	check("反证复核·重新解析原图并定位非法字符", status == 400 && e6.Field == "recheck" &&
		e6.Line == 2 && e6.Column == 3,
		fmt.Sprintf("status=%d body=%s", status, body))
}

func caseCounterServedByRealPage(base string) {
	// 真实页面必须已经挂载反证复核入口，且打包产物里包含对应调用。
	resp, err := client.Get(base + "/")
	if err != nil {
		check("反证复核·真实页面已挂载复核入口", false, err.Error())
		return
	}
	indexBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	indexHTML := string(indexBytes)
	rootOK := resp.StatusCode == 200 && strings.Contains(indexHTML, `id="root"`)

	re := regexp.MustCompile(`/assets/[^"']+\.js`)
	asset := re.FindString(indexHTML)
	bundleHas := false
	if asset != "" {
		jsResp, err := client.Get(base + asset)
		if err == nil {
			b, _ := io.ReadAll(jsResp.Body)
			jsResp.Body.Close()
			s := string(b)
			bundleHas = strings.Contains(s, "最小遮挡反证复核") &&
				strings.Contains(s, "/api/counter-evidence")
		}
	}
	check("反证复核·真实页面已挂载复核入口", rootOK && asset != "" && bundleHas,
		fmt.Sprintf("root=%v asset=%q bundleHasEntry=%v", rootOK, asset, bundleHas))
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

	// 最小遮挡负权矩形反证复核：真实页面同源入口的端到端用例
	caseCounterServedByRealPage(frontendURL)
	caseCounterFalsifiable(frontendURL)
	caseCounterNonFalsifiable(frontendURL)
	caseCounterValidation(frontendURL)

	// 直连后端，排除代理因素
	caseIdentityDirect(backendURL)

	fmt.Printf("[SUMMARY] verify 完成：%d 通过，%d 失败\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
