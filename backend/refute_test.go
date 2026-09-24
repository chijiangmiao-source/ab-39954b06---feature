package main

import (
	"math/rand"
	"testing"
)

// bruteRectangle 枚举全部 O(n^4) 个非空轴对齐矩形，与 minWeightRectangle
// 使用完全相同的裁决（面积 -> top -> left -> bottom -> right），作为精确参照。
func bruteRectangle(w [][]int, n, threshold int) *weightRectangle {
	var best *weightRectangle
	better := func(top, left, bottom, right, sum int) {
		cand := &weightRectangle{top, left, bottom, right, sum}
		if best == nil {
			best = cand
			return
		}
		a := (bottom - top + 1) * (right - left + 1)
		b := (best.bottom - best.top + 1) * (best.right - best.left + 1)
		if a < b ||
			(a == b &&
				(top < best.top ||
					(top == best.top && left < best.left) ||
					(top == best.top && left == best.left && bottom < best.bottom) ||
					(top == best.top && left == best.left && bottom == best.bottom && right < best.right))) {
			best = cand
		}
	}
	for top := 0; top < n; top++ {
		colSum := make([]int, n)
		for bottom := top; bottom < n; bottom++ {
			for c := 0; c < n; c++ {
				colSum[c] += w[bottom][c]
			}
			for left := 0; left < n; left++ {
				sum := 0
				for right := left; right < n; right++ {
					sum += colSum[right]
					if sum >= threshold {
						better(top, left, bottom, right, sum)
					}
				}
			}
		}
	}
	return best
}

func randomWeights(rng *rand.Rand, n int) [][]int {
	w := make([][]int, n)
	for r := range w {
		w[r] = make([]int, n)
		for c := range w[r] {
			// 让 0 占多数（模拟“仅参考缺陷处有权重”），并混入大量负权。
			switch rng.Intn(5) {
			case 0:
				w[r][c] = 1
			case 1:
				w[r][c] = -1
			}
		}
	}
	return w
}

func TestMinWeightRectangleMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(20240924))
	sizes := []int{1, 2, 3, 5, 8, 12}
	for iter := 0; iter < 200; iter++ {
		n := sizes[rng.Intn(len(sizes))]
		w := randomWeights(rng, n)
		threshold := 1 + rng.Intn(4)
		got := minWeightRectangle(w, n, threshold)
		want := bruteRectangle(w, n, threshold)
		if (got == nil) != (want == nil) {
			t.Fatalf("iter=%d n=%d K=%d: 存在性不一致 got=%v want=%v", iter, n, threshold, got, want)
		}
		if got != nil && *got != *want {
			t.Fatalf("iter=%d n=%d K=%d: 最小矩形不一致 got=%+v want=%+v", iter, n, threshold, got, want)
		}
	}
}

// TestNegativeWeights 构造负权陷阱：短窗口因负权失败，更长窗口才能凑够阈值，
// 贪心/滑窗会选错；精确算法必须与暴力参照一致。
func TestNegativeWeights(t *testing.T) {
	n := 6
	w := make([][]int, n)
	for r := range w {
		w[r] = make([]int, n)
	}
	// 单行序列：1, -1, 1, 1 -> K=2 时最短短数组是 [2..3]（长度2）；
	// 长度 1 最大为 1，贪心扩窗会先吃到 -1。
	w[0][0] = 1
	w[0][1] = -1
	w[0][2] = 1
	w[0][3] = 1
	got := minWeightRectangle(w, n, 2)
	want := bruteRectangle(w, n, 2)
	if got == nil || want == nil || *got != *want {
		t.Fatalf("负权用例裁决错误 got=%+v want=%+v", got, want)
	}
	if got.top != 0 || got.left != 2 || got.bottom != 0 || got.right != 3 {
		t.Fatalf("期望单行窗口 [0,2..0,3]，得到 %+v", got)
	}

	// 全部为负：任何窗口和都 < 1，必须返回 nil（不可反证，不伪造矩形）。
	for r := range w {
		for c := range w[r] {
			w[r][c] = -1
		}
	}
	if got := minWeightRectangle(w, n, 1); got != nil {
		t.Fatalf("全负权不应存在窗口，得到 %+v", got)
	}
}

func TestRectangleTieBreakOrder(t *testing.T) {
	// 两个面积相同、权重相同的候选窗口，按 top,left,bottom,right 顺序取前者。
	n := 5
	mk := func() [][]int {
		w := make([][]int, n)
		for r := range w {
			w[r] = make([]int, n)
		}
		return w
	}
	// 候选 A：(1,0) 单格；候选 B：(0,1) 单格，面积都为 1 -> top 小者 B(0,1) 胜。
	w := mk()
	w[1][0] = 1
	w[0][1] = 1
	got := minWeightRectangle(w, n, 1)
	if got == nil || got.top != 0 || got.left != 1 {
		t.Fatalf("同面积应取 top 更小者，得到 %+v", got)
	}

	// 同 top：(0,2) 与 (0,4) -> left 小者胜。
	w = mk()
	w[0][2] = 1
	w[0][4] = 1
	got = minWeightRectangle(w, n, 1)
	if got == nil || got.top != 0 || got.left != 2 {
		t.Fatalf("同 top 应取 left 更小者，得到 %+v", got)
	}
}

// makeBinaryMatrix 从点集构造 n×n 矩阵。
func makeBinaryMatrix(n int, pts [][2]int) [][]uint8 {
	m := make([][]uint8, n)
	for r := range m {
		m[r] = make([]uint8, n)
	}
	for _, p := range pts {
		m[p[0]][p[1]] = 1
	}
	return m
}

// TestRunRefuteRefutable 构造可反证场景：规范候选 identity 以 6:5 领先目标候选 rot90/(0,0)，
// 但存在一个小局部窗口，其中仅规范命中点比仅目标命中点多 2，
// 移除其中参考缺陷后目标反超为 5:4。
func TestRunRefuteRefutable(t *testing.T) {
	const n = 16
	// 参考点：
	//  局部窗口内 (2,2)、(3,3) 仅被 identity 命中；(4,4) 仅被 rot90 命中（-1 点）；
	//  另有 3 个两候选都命中的公共点。
	refPts := [][2]int{{2, 2}, {3, 3}, {4, 4}, {8, 8}, {9, 9}, {10, 10}}
	ref := makeBinaryMatrix(n, refPts)

	// rot90 下复检点 (r,c) -> (c, n-1-r)；要命中参考点 (R,C)，复检点需为 (n-1-C, R)。
	recPts := [][2]int{
		{2, 2}, {3, 3}, // identity 命中 (2,2),(3,3)；rot90 不命中
		{8, 8}, {9, 9}, {10, 10}, // identity 命中这三个公共点
		{7, 8}, {6, 9}, {5, 10}, // 仅 rot90 命中 (8,8),(9,9),(10,10)
	}
	// 复检点 (n-1-4, 4)=(11,4) 经 rot90 命中参考点 (4,4)；identity 下为 (11,4) 不命中。
	recPts = append(recPts, [2]int{n - 1 - 4, 4})
	rec := makeBinaryMatrix(n, recPts)

	// 预检：identity 重合 5（(2,2),(3,3),三个公共点），rot90/(0,0) 重合 4。
	if c := countOverlapDirect(ref, rec, n, 0, 0, 0); c != 5 {
		t.Fatalf("构造失败：identity 重合应为 5，得到 %d", c)
	}
	if c := countOverlapDirect(ref, rec, n, 1, 0, 0); c != 4 {
		t.Fatalf("构造失败：rot90 重合应为 4，得到 %d", c)
	}

	resp, err := runRefute(ref, rec, n, 1, 0, 0) // 目标候选 rot90/(0,0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Canonical.PoseIndex != 0 || resp.Canonical.Overlap != 5 {
		t.Fatalf("规范候选应为 identity 重合 5，得到 %+v", resp.Canonical)
	}
	if !resp.Refutable || resp.Window == nil {
		t.Fatalf("应为可反证，得到 refutable=%v window=%v", resp.Refutable, resp.Window)
	}
	if resp.After == nil || resp.After.Target != 4 || resp.After.Canonical != 3 {
		t.Fatalf("移除后目标应以 4:3 反超，得到 before=%+v after=%+v", resp.Before, resp.After)
	}
	// 最小窗口必须覆盖对角分布的两个仅规范命中点 (2,2),(3,3)：2x2 面积 4，
	// 不含 -1 点 (4,4)；检查窗口确实覆盖两个 +1 点。
	plusInWindow := 0
	for _, p := range resp.Weights.Plus {
		if p[0] >= resp.Window.Top && p[0] <= resp.Window.Bottom &&
			p[1] >= resp.Window.Left && p[1] <= resp.Window.Right {
			plusInWindow++
		}
	}
	if plusInWindow != 2 {
		t.Fatalf("窗口内应含 2 个 +1 点，得到 %d，window=%+v", plusInWindow, resp.Window)
	}
	// 被移除的参考缺陷必须全部位于窗口内。
	for _, p := range resp.Window.Removed {
		if p[0] < resp.Window.Top || p[0] > resp.Window.Bottom ||
			p[1] < resp.Window.Left || p[1] > resp.Window.Right {
			t.Fatalf("移除点 %v 不在窗口 %+v 内", p, resp.Window)
		}
	}
	// 权重编码必须是 {-1,0,1}，且 plus 数减去 minus 数 = C - T。
	diff := resp.Canonical.Overlap - resp.Target.Overlap
	if len(resp.Weights.Plus)-len(resp.Weights.Minus) != diff {
		t.Fatalf("权重差编码错误：plus=%d minus=%d C-T=%d",
			len(resp.Weights.Plus), len(resp.Weights.Minus), diff)
	}
	if len(resp.Weights.Minus) != 1 {
		t.Fatalf("应恰有 1 个仅目标命中点，得到 %d", len(resp.Weights.Minus))
	}
}

// TestRunRefuteImpossible 构造不可反证场景：目标候选一个参考缺陷都不命中，
// 且移除任何参考缺陷都无法让目标胜出（移除只减不增）。
func TestRunRefuteImpossible(t *testing.T) {
	const n = 16
	refPts := [][2]int{{2, 2}, {2, 3}, {7, 8}}
	ref := makeBinaryMatrix(n, refPts)
	// 复检点放在与所有参考点在 identity 全平移最优解下仍命中 3，
	// 但指定的目标候选 rot270/(+14,+14) 重合为 0 且无法靠移除翻盘。
	rec := makeBinaryMatrix(n, refPts)
	resp, err := runRefute(ref, rec, n, 3, 14, 14)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Refutable || resp.Window != nil || resp.After != nil {
		t.Fatalf("应为不可反证且不伪造窗口，得到 %+v", resp)
	}
}

// TestRefuteRecoversRegistration 验证反证入口确实重跑了既有精确配准：
// 规范候选字段必须与穷举搜索一致。
func TestRefuteRecoversRegistration(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	n := 32
	ref := randomMatrix(rng, n, 0.08)
	rec := randomMatrix(rng, n, 0.08)
	want := searchMaxOverlap(ref, rec, n)
	resp, err := runRefute(ref, rec, n, 5, 1, -2)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Canonical.PoseIndex != want.poseIndex ||
		resp.Canonical.Dy != want.dy || resp.Canonical.Dx != want.dx ||
		resp.Canonical.Overlap != want.maxOverlap {
		t.Fatalf("规范候选与重跑配准不一致：resp=%+v want=%+v", resp.Canonical, want)
	}
}
