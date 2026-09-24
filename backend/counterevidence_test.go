package main

import (
	"math/rand"
	"testing"
)

// bruteMinRectangle 独立暴力参照：枚举全部轴对齐非空矩形，
// 与 minWeightRectangle 的精确结果逐字段比较。
func bruteMinRectangle(w []int, n, threshold int) ([4]int, bool) {
	best := [4]int{}
	found := false
	bestArea := 0
	for t := 0; t < n; t++ {
		for b := t; b < n; b++ {
			for l := 0; l < n; l++ {
				sum := 0
				for r := l; r < n; r++ {
					for i := t; i <= b; i++ {
						sum += w[i*n+r]
					}
					if sum >= threshold {
						cand := [4]int{t, l, b, r}
						area := (b - t + 1) * (r - l + 1)
						if !found || area < bestArea ||
							(area == bestArea && lexLess(cand, best)) {
							found = true
							bestArea = area
							best = cand
						}
					}
				}
			}
		}
	}
	return best, found
}

func TestMinWeightRectangleVsBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	for trial := 0; trial < 400; trial++ {
		n := 1 + rng.Intn(14)
		w := make([]int, n*n)
		for i := range w {
			// 以参考缺陷的实际取值 -1/0/1 为主，混入少量更大权值。
			switch rng.Intn(10) {
			case 0:
				w[i] = -1
			case 1, 2:
				w[i] = 1
			case 3:
				w[i] = -rng.Intn(3)
			case 4:
				w[i] = rng.Intn(3)
			}
		}
		threshold := 1 + rng.Intn(5)
		gotT, gotL, gotB, gotR, gotOK := minWeightRectangle(w, n, threshold)
		want, wantOK := bruteMinRectangle(w, n, threshold)
		if gotOK != wantOK {
			t.Fatalf("trial=%d n=%d threshold=%d: ok 不一致 got=%v want=%v", trial, n, threshold, gotOK, wantOK)
		}
		if gotOK {
			got := [4]int{gotT, gotL, gotB, gotR}
			if got != want {
				t.Fatalf("trial=%d n=%d threshold=%d: 窗口不一致 got=%v want=%v", trial, n, threshold, got, want)
			}
		}
	}
}

// TestMinWeightRectangleNegativeWeights 构造含负权的场景：
// 可行最窄区间必须跨过负权列才能连通两端的正权列，
// “伸右端、缩左端”的贪心滑窗（依赖前缀单调）会漏掉它。
func TestMinWeightRectangleNegativeWeights(t *testing.T) {
	n := 5
	w := make([]int, n*n)
	// 单行：列 0 = +2，列 1 = -1，列 2 = +2；threshold = 3。
	// 任何不含负权列的区间最大和为 2；唯一最省面积的可行区间是 [0..2]（和 3）。
	w[0*n+0] = 2
	w[0*n+1] = -1
	w[0*n+2] = 2
	gotT, gotL, gotB, gotR, ok := minWeightRectangle(w, n, 3)
	if !ok {
		t.Fatal("应找到跨负权列的可行矩形")
	}
	if gotT != 0 || gotL != 0 || gotB != 0 || gotR != 2 {
		t.Fatalf("窗口应为单行 [0,0]-[0,2]，得到 [%d,%d]-[%d,%d]", gotT, gotL, gotB, gotR)
	}
}

func TestMinWeightRectangleNone(t *testing.T) {
	n := 4
	w := make([]int, n*n)
	w[0] = 1
	if _, _, _, _, ok := minWeightRectangle(w, n, 2); ok {
		t.Fatal("总权值不足时不应存在可行矩形")
	}
}

func TestMinWeightRectangleTieByCoordinates(t *testing.T) {
	// 两个不相邻的单点 +1，threshold=1：面积同为 1 时
	// 应按 (上, 左) 裁决到 (0,0)，而不是 (2,3)。
	n := 4
	w := make([]int, n*n)
	w[2*n+3] = 1
	w[0*n+0] = 1
	gotT, gotL, gotB, gotR, ok := minWeightRectangle(w, n, 1)
	if !ok || gotT != 0 || gotL != 0 || gotB != 0 || gotR != 0 {
		t.Fatalf("同面积应按上、左裁决，得到 [%d,%d]-[%d,%d] ok=%v", gotT, gotL, gotB, gotR, ok)
	}
}

// makeUInt8Matrix 构造 n×n 零矩阵。
func makeUInt8Matrix(n int) [][]uint8 {
	m := make([][]uint8, n)
	for r := range m {
		m[r] = make([]uint8, n)
	}
	return m
}

// buildFalsifiableFixture 构造一个确定性的可反证场景：
//   - 共享命中 S（两候选都命中，权 0）：(3,3),(3,7),(3,11)
//   - 规范独占 X（权 +1）：(10,4),(10,5),(10,6)
//   - 目标独占 Y（权 -1）：(14,1),(14,15)
//
// 规范候选 (identity, dy=+1, dx=0) 命中 6 点，目标候选 (identity, 0,0) 命中 5 点；
// gap=1，阈值 2，最小窗口为覆盖 (10,4),(10,5) 的 1×2 矩形（面积 2，同面积取最左）。
func buildFalsifiableFixture() (ref, rec [][]uint8, n int) {
	n = 16
	ref = makeUInt8Matrix(n)
	rec = makeUInt8Matrix(n)
	shared := [][2]int{{3, 3}, {3, 7}, {3, 11}}
	xOnly := [][2]int{{10, 4}, {10, 5}, {10, 6}}
	yOnly := [][2]int{{14, 1}, {14, 15}}
	for _, p := range shared {
		ref[p[0]][p[1]] = 1
		rec[p[0]][p[1]] = 1   // 目标 dy=0 命中
		rec[p[0]-1][p[1]] = 1 // 规范 dy=+1 命中
	}
	for _, p := range xOnly {
		ref[p[0]][p[1]] = 1
		rec[p[0]-1][p[1]] = 1 // 仅规范 dy=+1 命中
	}
	for _, p := range yOnly {
		ref[p[0]][p[1]] = 1
		rec[p[0]][p[1]] = 1 // 仅目标 dy=0 命中
	}
	return ref, rec, n
}

func TestCounterEvidenceFalsifiable(t *testing.T) {
	ref, rec, n := buildFalsifiableFixture()
	const pose, dy, dx = 0, 0, 0

	// 规范候选以独立暴力穷举核对，确保服务端自行重算而非信任调用方。
	want := bruteForce(ref, rec, n)

	resp, err := runCounterEvidence(ref, rec, n, pose, dy, dx)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Canonical.PoseIndex != want.poseIndex || resp.Canonical.Dy != want.dy ||
		resp.Canonical.Dx != want.dx || resp.Canonical.Overlap != want.maxOverlap {
		t.Fatalf("规范候选与暴力穷举不一致：%+v want=%+v", resp.Canonical, want)
	}
	if want.poseIndex != 0 || want.dy != 1 || want.dx != 0 || want.maxOverlap != 6 {
		t.Fatalf("构造预期规范候选为 identity/(1,0)/6，得到 %+v", want)
	}
	if !resp.Feasible || resp.Window == nil {
		t.Fatal("该构造应可反证")
	}
	if resp.Target.Overlap != 5 || resp.Gap != 1 {
		t.Fatalf("目标重合应为 5、gap=1，得到 target=%d gap=%d", resp.Target.Overlap, resp.Gap)
	}

	// 独立按定义重建权值网格并用暴力参照核对窗口。
	weights := make([]int, n*n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if ref[r][c] == 0 {
				continue
			}
			ch := 0
			if countOverlapAt(ref, rec, n, want.poseIndex, want.dy, want.dx, r, c) {
				ch++
			}
			if countOverlapAt(ref, rec, n, pose, dy, dx, r, c) {
				ch--
			}
			weights[r*n+c] = ch
		}
	}
	wantWin, found := bruteMinRectangle(weights, n, resp.Gap+1)
	if !found {
		t.Fatal("暴力参照认为不可行，与实现矛盾")
	}
	w := resp.Window
	got := [4]int{w.Top, w.Left, w.Bottom, w.Right}
	if got != wantWin {
		t.Fatalf("窗口与暴力参照不一致：got=%v want=%v", got, wantWin)
	}
	if wantWin != [4]int{10, 4, 10, 5} {
		t.Fatalf("构造预期窗口 [10,4]-[10,5]，暴力参照给出 %v", wantWin)
	}
	if w.Area != 2 || w.RemovedCanonical != 2 || w.RemovedTarget != 0 {
		t.Fatalf("窗口属性异常：area=%d remC=%d remT=%d", w.Area, w.RemovedCanonical, w.RemovedTarget)
	}
	if w.CanonicalAfter != 4 || w.TargetAfter != 5 || w.TargetAfter <= w.CanonicalAfter {
		t.Fatalf("移除后目标必须严格胜出：C'=%d T'=%d", w.CanonicalAfter, w.TargetAfter)
	}
	if len(w.RemovedPoints) != 2 {
		t.Fatalf("应列出 2 个被移除的参考缺陷，得到 %d", len(w.RemovedPoints))
	}
}

// countOverlapAt 判断参考缺陷 (tr,tc) 是否被某候选（姿态 p、平移 ty,tx）命中。
func countOverlapAt(ref, rec [][]uint8, n, p, ty, tx, tr, tc int) bool {
	if ref[tr][tc] == 0 {
		return false
	}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(p, r, c, n)
			if pr+ty == tr && pc+tx == tc {
				return true
			}
		}
	}
	return false
}

func TestCounterEvidenceNonFalsifiable(t *testing.T) {
	// 12 个规范独占点，目标候选 0 命中：阈值 gap+1=13，
	// 而全图正权总和只有 12，几何上不存在任何可行矩形。
	n := 16
	ref := makeUInt8Matrix(n)
	rec := makeUInt8Matrix(n)
	pts := [][2]int{
		{2, 3}, {3, 9}, {4, 4}, {5, 12}, {6, 7}, {7, 11},
		{8, 5}, {9, 10}, {10, 13}, {11, 2}, {12, 8}, {13, 6},
	}
	for _, p := range pts {
		ref[p[0]][p[1]] = 1
		rec[p[0]-1][p[1]] = 1 // 仅 dy=+1 的规范候选命中
	}
	want := bruteForce(ref, rec, n)
	resp, err := runCounterEvidence(ref, rec, n, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Canonical.PoseIndex != want.poseIndex || resp.Canonical.Dy != want.dy ||
		resp.Canonical.Dx != want.dx || resp.Canonical.Overlap != want.maxOverlap {
		t.Fatalf("规范候选与暴力穷举不一致：%+v want=%+v", resp.Canonical, want)
	}
	if resp.Feasible || resp.Window != nil {
		t.Fatalf("该构造不可反证，却返回窗口：%+v", resp.Window)
	}
	if want.maxOverlap != 12 || resp.Target.Overlap != 0 || resp.Gap != 12 {
		t.Fatalf("重合数异常：canonical=%d target=%d gap=%d", want.maxOverlap, resp.Target.Overlap, resp.Gap)
	}
}

func TestCounterEvidenceSameCandidateRejected(t *testing.T) {
	n := 16
	ref := makeUInt8Matrix(n)
	rec := makeUInt8Matrix(n)
	ref[3][3] = 1
	rec[3][3] = 1
	_, err := runCounterEvidence(ref, rec, n, 0, 0, 0)
	if err == nil {
		t.Fatal("目标候选与规范候选相同时应拒绝")
	}
	if ie, ok := err.(*InputError); !ok || ie.Field != "targetPose" {
		t.Fatalf("应返回定位到 targetPose 的输入错误，得到 %v", err)
	}
}
