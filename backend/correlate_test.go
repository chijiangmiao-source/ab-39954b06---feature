package main

import (
	"math/rand"
	"testing"
)

// bruteForce 逐姿态逐平移直接计数，是 FFT 相关搜索的精确参照。
func bruteForce(ref, rec [][]uint8, n int) searchResult {
	res := searchResult{maxOverlap: -1}
	for p := 0; p < 8; p++ {
		for dy := -(n - 1); dy <= n-1; dy++ {
			for dx := -(n - 1); dx <= n-1; dx++ {
				v := countOverlapDirect(ref, rec, n, p, dy, dx)
				if v > res.maxOverlap {
					res = searchResult{maxOverlap: v, tieCount: 1, poseIndex: p, dy: dy, dx: dx}
				} else if v == res.maxOverlap {
					res.tieCount++
				}
			}
		}
	}
	return res
}

func randomMatrix(rng *rand.Rand, n int, density float64) [][]uint8 {
	m := make([][]uint8, n)
	for r := range m {
		m[r] = make([]uint8, n)
		for c := range m[r] {
			if rng.Float64() < density {
				m[r][c] = 1
			}
		}
	}
	return m
}

func TestSearchMatchesBruteForce(t *testing.T) {
	sizes := []int{16, 17, 24, 33}
	densities := []float64{0.05, 0.3, 0.8}
	rng := rand.New(rand.NewSource(42))
	for _, n := range sizes {
		for _, d := range densities {
			ref := randomMatrix(rng, n, d)
			rec := randomMatrix(rng, n, d)
			got := searchMaxOverlap(ref, rec, n)
			want := bruteForce(ref, rec, n)
			if got != want {
				t.Fatalf("n=%d d=%.2f: FFT 搜索 %+v，暴力参照 %+v", n, d, got, want)
			}
		}
	}
}

func TestAllOnesDense(t *testing.T) {
	n := 16
	ref := randomMatrix(rand.New(rand.NewSource(1)), n, 1.0)
	rec := randomMatrix(rand.New(rand.NewSource(2)), n, 1.0)
	got := searchMaxOverlap(ref, rec, n)
	if got.maxOverlap != n*n {
		t.Fatalf("全 1 矩阵最大重合应为 %d，得到 %d", n*n, got.maxOverlap)
	}
	if got.tieCount != 8 {
		t.Fatalf("全 1 矩阵每种姿态仅在 (0,0) 达到最大，并列数应为 8，得到 %d", got.tieCount)
	}
	if got.poseIndex != 0 || got.dy != 0 || got.dx != 0 {
		t.Fatalf("规范解应为 identity/(0,0)，得到 pose=%d dy=%d dx=%d", got.poseIndex, got.dy, got.dx)
	}
}

func TestConstructedTransformRecovered(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	n := 64
	// 在中央区域布点，保证姿态+平移后全部留在画布内。
	ref := make([][]uint8, n)
	for r := range ref {
		ref[r] = make([]uint8, n)
	}
	pts := 0
	for pts < 200 {
		r := 8 + rng.Intn(48)
		c := 8 + rng.Intn(48)
		if ref[r][c] == 0 {
			ref[r][c] = 1
			pts++
		}
	}
	const pose, dy, dx = 3, 5, -7
	rec := make([][]uint8, n)
	for r := range rec {
		rec[r] = make([]uint8, n)
	}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if ref[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(pose, r, c, n)
			tr, tc := pr+dy, pc+dx
			if tr < 0 || tr >= n || tc < 0 || tc >= n {
				t.Fatalf("构造点 (%d,%d) 移出画布", tr, tc)
			}
			rec[tr][tc] = 1
		}
	}
	got := searchMaxOverlap(ref, rec, n)
	if got.maxOverlap != 200 {
		t.Fatalf("构造 200 点应全部重合，得到 %d", got.maxOverlap)
	}
	// 用规范解把复检图映射回去，必须与参考图完全一致。
	if countOverlapDirect(ref, rec, n, got.poseIndex, got.dy, got.dx) != 200 {
		t.Fatal("规范解整数复核失败")
	}
}

func TestTieBreakOrder(t *testing.T) {
	// 两个关于中心对称的点：identity 与 rot180 都能以相反平移达到 2，
	// 规范解必须落在姿态下标更小的 identity 上。
	n := 16
	ref := make([][]uint8, n)
	for r := range ref {
		ref[r] = make([]uint8, n)
	}
	ref[4][5] = 1
	ref[11][10] = 1
	rec := make([][]uint8, n)
	for r := range rec {
		rec[r] = make([]uint8, n)
	}
	rec[4][5] = 1
	rec[11][10] = 1
	got := searchMaxOverlap(ref, rec, n)
	want := bruteForce(ref, rec, n)
	if got != want {
		t.Fatalf("平局裁决不一致：FFT %+v，暴力 %+v", got, want)
	}
	if got.poseIndex != 0 || got.dy != 0 || got.dx != 0 {
		t.Fatalf("规范解应为 identity/(0,0)，得到 %+v", got)
	}
	if got.tieCount < 2 {
		t.Fatalf("对称点阵应存在并列最优，得到 tieCount=%d", got.tieCount)
	}
}

func TestOutOfCanvasStillCounted(t *testing.T) {
	// 复检图边缘点经规范平移后移出画布：不计入重合，但计入复检图总数。
	n := 16
	ref := make([][]uint8, n)
	rec := make([][]uint8, n)
	for r := 0; r < n; r++ {
		ref[r] = make([]uint8, n)
		rec[r] = make([]uint8, n)
	}
	cluster := [][2]int{{2, 3}, {3, 9}, {4, 4}, {6, 12}, {7, 7}, {9, 11}, {11, 5}, {12, 10}, {13, 13}, {5, 2}, {10, 8}, {8, 6}}
	for _, p := range cluster {
		rec[p[0]][p[1]] = 1
		ref[p[0]+2][p[1]+1] = 1 // 参考图 = 复检图平移 (+2,+1)
	}
	rec[15][15] = 1 // 边缘噪声点，平移后移出画布
	resp, err := runAudit(ref, rec, n)
	if err != nil {
		t.Fatal(err)
	}
	if resp.RecheckCount != len(cluster)+1 {
		t.Fatalf("复检图总数应含移出画布的缺陷：期望 %d，得到 %d", len(cluster)+1, resp.RecheckCount)
	}
	if resp.MaxOverlap != len(cluster) {
		t.Fatalf("最大重合应为 %d，得到 %d", len(cluster), resp.MaxOverlap)
	}
	if resp.Overlay.RecheckOut != 1 {
		t.Fatalf("应有 1 个移出画布的复检缺陷，得到 %d", resp.Overlay.RecheckOut)
	}
	if len(resp.Overlay.Matched) != resp.MaxOverlap {
		t.Fatalf("matched 数 %d 与最大重合 %d 不一致", len(resp.Overlay.Matched), resp.MaxOverlap)
	}
	if len(resp.Overlay.RecheckOnly) != resp.RecheckCount-resp.MaxOverlap {
		t.Fatal("recheckOnly 计数不一致")
	}
}

func TestLargeDense512(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过满尺寸稠密用例")
	}
	n := 512
	ref := make([][]uint8, n)
	rec := make([][]uint8, n)
	for r := 0; r < n; r++ {
		ref[r] = make([]uint8, n)
		rec[r] = make([]uint8, n)
		for c := 0; c < n; c++ {
			ref[r][c] = 1
			rec[r][c] = 1
		}
	}
	got := searchMaxOverlap(ref, rec, n)
	if got.maxOverlap != n*n || got.tieCount != 8 || got.poseIndex != 0 || got.dy != 0 || got.dx != 0 {
		t.Fatalf("512 稠密结果异常：%+v", got)
	}
}
