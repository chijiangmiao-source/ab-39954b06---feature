package main

import (
	"errors"
	"time"
)

// refuteMaxN 是最小遮挡反证复核入口支持的最大边长（最小边长沿用 parseMatrix 的 minN=16）。
// 复核需要逐参考缺陷编码权重并枚举矩形，故入口限定 16..96；/api/audit 的 16..512 不变。
const refuteMaxN = 96

type refuteRequest struct {
	Reference string `json:"reference"`
	Recheck   string `json:"recheck"`
	// 指针用于区分“字段缺失/类型错误”与“显式传入 0”。
	PoseIndex *int `json:"poseIndex"`
	Dy        *int `json:"dy"`
	Dx        *int `json:"dx"`
}

type refuteCandidateInfo struct {
	PoseIndex int    `json:"poseIndex"`
	Pose      string `json:"pose"`
	PoseLabel string `json:"poseLabel"`
	Dy        int    `json:"dy"`
	Dx        int    `json:"dx"`
	Overlap   int    `json:"overlap"`
}

type refuteWindowInfo struct {
	Top     int     `json:"top"`
	Left    int     `json:"left"`
	Bottom  int     `json:"bottom"`
	Right   int     `json:"right"`
	Area    int     `json:"area"`
	Sum     int     `json:"sum"`
	Removed []point `json:"removed"`
}

type refuteCountPair struct {
	Canonical int `json:"canonical"`
	Target    int `json:"target"`
}

type refuteResponse struct {
	N         int                 `json:"n"`
	Canonical refuteCandidateInfo `json:"canonical"`
	Target    refuteCandidateInfo `json:"target"`
	Refutable bool                `json:"refutable"`
	// 不可反证时 Window/After 均为 nil，绝不伪造矩形。
	Window *refuteWindowInfo `json:"window"`
	Before refuteCountPair    `json:"before"`
	After  *refuteCountPair   `json:"after"`
	// weights 仅用于前端逐点标注证据：minus=-1（仅目标命中）、zero=0、plus=1（仅规范命中）。
	Weights   refuteWeightInfo `json:"weights"`
	ElapsedMs int64            `json:"elapsedMs"`
}

type refuteWeightInfo struct {
	Minus []point `json:"minus"`
	Zero  []point `json:"zero"`
	Plus  []point `json:"plus"`
}

// runRefute 执行最小遮挡反证复核：
//  1. 重新做既有精确配准（FFT 穷举 + 整数复核），得到规范候选与其重合数；
//  2. 纯整数计数目标候选重合数；
//  3. 逐参考缺陷编码命中差 w = 规范命中 - 目标命中 ∈ {-1,0,1}；
//  4. 求面积最小的轴对齐非空矩形，使移除其中参考缺陷后目标严格胜过规范；
//     不存在这样的矩形时返回 refutable=false，不附带任何窗口。
func runRefute(ref, rec [][]uint8, n, tp, tdy, tdx int) (*refuteResponse, error) {
	start := time.Now()

	// 既有精确配准必须在服务端重跑，不能信任浏览器保存的最大重合或变换。
	res := searchMaxOverlap(ref, rec, n)
	cOverlap := countOverlapDirect(ref, rec, n, res.poseIndex, res.dy, res.dx)
	if cOverlap != res.maxOverlap {
		return nil, errors.New("内部校验失败：规范候选整数复核与相关结果不一致")
	}
	tOverlap := countOverlapDirect(ref, rec, n, tp, tdy, tdx)

	canonicalHit := markHits(ref, rec, n, res.poseIndex, res.dy, res.dx)
	targetHit := markHits(ref, rec, n, tp, tdy, tdx)

	weights := make([][]int, n)
	wi := refuteWeightInfo{
		Minus: make([]point, 0),
		Zero:  make([]point, 0),
		Plus:  make([]point, 0),
	}
	for r := 0; r < n; r++ {
		weights[r] = make([]int, n)
		for c := 0; c < n; c++ {
			if ref[r][c] == 0 {
				continue
			}
			a, b := 0, 0
			if canonicalHit[r*n+c] {
				a = 1
			}
			if targetHit[r*n+c] {
				b = 1
			}
			w := a - b // 规范命中差：仅规范命中=+1，仅目标命中=-1，一致=0
			weights[r][c] = w
			p := point{r, c}
			switch w {
			case -1:
				wi.Minus = append(wi.Minus, p)
			case 0:
				wi.Zero = append(wi.Zero, p)
			default:
				wi.Plus = append(wi.Plus, p)
			}
		}
	}

	resp := &refuteResponse{
		N: n,
		Canonical: refuteCandidateInfo{
			PoseIndex: res.poseIndex,
			Pose:      poses[res.poseIndex].Name,
			PoseLabel: poses[res.poseIndex].Label,
			Dy:        res.dy,
			Dx:        res.dx,
			Overlap:   cOverlap,
		},
		Target: refuteCandidateInfo{
			PoseIndex: tp,
			Pose:      poses[tp].Name,
			PoseLabel: poses[tp].Label,
			Dy:        tdy,
			Dx:        tdx,
			Overlap:   tOverlap,
		},
		Before: refuteCountPair{Canonical: cOverlap, Target: tOverlap},
		Weights: wi,
	}

	// 移除窗口 U 后：C' = C - 仅规范命中数(U)，T' = T - 仅目标命中数(U)，
	// 目标严格胜出要求 Σ_U(规范命中-目标命中) >= C - T + 1。
	threshold := cOverlap - tOverlap + 1
	if threshold < 1 {
		// 理论上不会发生：规范候选是穷举全局最优，C >= T 恒成立。
		return nil, errors.New("内部校验失败：目标候选重合数超过规范候选")
	}
	rect := minWeightRectangle(weights, n, threshold)
	if rect == nil {
		resp.ElapsedMs = time.Since(start).Milliseconds()
		return resp, nil
	}

	removed := make([]bool, n*n)
	removedList := make([]point, 0)
	for r := rect.top; r <= rect.bottom; r++ {
		for c := rect.left; c <= rect.right; c++ {
			if ref[r][c] != 0 {
				removed[r*n+c] = true
				removedList = append(removedList, point{r, c})
			}
		}
	}
	cAfter := countOverlapAfterRemoval(ref, rec, n, res.poseIndex, res.dy, res.dx, removed)
	tAfter := countOverlapAfterRemoval(ref, rec, n, tp, tdy, tdx, removed)
	// 纯整数复核：命中差变化必须与窗口权重和一致，且目标确实严格胜出。
	// 注意窗口可能包含 -1 点（仅目标命中），它们不减少规范重合，故不能用 C-sum 反推 C'。
	if tAfter-cAfter != (tOverlap-cOverlap)+rect.sum {
		return nil, errors.New("内部校验失败：移除后命中差与窗口权重和不一致")
	}
	if tAfter <= cAfter {
		return nil, errors.New("内部校验失败：最小窗口未使目标候选严格胜出")
	}

	resp.Refutable = true
	resp.Window = &refuteWindowInfo{
		Top:     rect.top,
		Left:    rect.left,
		Bottom:  rect.bottom,
		Right:   rect.right,
		Area:    (rect.bottom - rect.top + 1) * (rect.right - rect.left + 1),
		Sum:     rect.sum,
		Removed: removedList,
	}
	resp.After = &refuteCountPair{Canonical: cAfter, Target: tAfter}
	resp.ElapsedMs = time.Since(start).Milliseconds()
	return resp, nil
}

// markHits 返回参考图中被“复检图经姿态 p + 平移 (dy,dx)”命中的缺陷标记。
func markHits(ref, rec [][]uint8, n, p, dy, dx int) []bool {
	hit := make([]bool, n*n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(p, r, c, n)
			tr, tc := pr+dy, pc+dx
			if tr >= 0 && tr < n && tc >= 0 && tc < n && ref[tr][tc] != 0 {
				hit[tr*n+tc] = true
			}
		}
	}
	return hit
}

// countOverlapAfterRemoval 在屏蔽 removed 标记的参考缺陷后纯整数计数重合。
func countOverlapAfterRemoval(ref, rec [][]uint8, n, p, dy, dx int, removed []bool) int {
	cnt := 0
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(p, r, c, n)
			tr, tc := pr+dy, pc+dx
			if tr >= 0 && tr < n && tc >= 0 && tc < n &&
				ref[tr][tc] != 0 && !removed[tr*n+tc] {
				cnt++
			}
		}
	}
	return cnt
}

// weightRectangle 是一个轴对齐非空矩形及其权重和（含负权）。
type weightRectangle struct {
	top, left, bottom, right, sum int
}

// minWeightRectangle 在 n×n 权重矩阵（元素 ∈ {-1,0,1}，空单元格为 0）中，
// 精确找出权重和 >= threshold 的面积最小轴对齐非空矩形；
// 同面积按 上(top)、左(left)、下(bottom)、右(right) 坐标升序裁决。
//
// 算法：枚举上下边界 O(n²)，把行间题归约为“和 >= K 的最短子数组”。
// 权重含负值时双指针/贪心滑窗不成立（扩展窗口可能先减后增），
// 这里对每个列前缀和维护“候选起点单调栈 + 二分”精确求解 O(n)：
// 若 i1<i2 且 pref[i1]>=pref[i2]，则对任意右端点 i2 都严格优于 i1
// （子数组更短且和不更小），故 i1 永不会成为最优起点，可安全弹出。
func minWeightRectangle(w [][]int, n, threshold int) *weightRectangle {
	var best *weightRectangle
	consider := func(top, left, bottom, right, sum int) {
		if best == nil {
			best = &weightRectangle{top, left, bottom, right, sum}
			return
		}
		area := (bottom - top + 1) * (right - left + 1)
		bestArea := (best.bottom - best.top + 1) * (best.right - best.left + 1)
		if area < bestArea ||
			(area == bestArea &&
				(top < best.top ||
					(top == best.top && left < best.left) ||
					(top == best.top && left == best.left && bottom < best.bottom) ||
					(top == best.top && left == best.left && bottom == best.bottom && right < best.right))) {
			best = &weightRectangle{top, left, bottom, right, sum}
		}
	}

	colSum := make([]int, n)
	pref := make([]int, n+1)
	for top := 0; top < n; top++ {
		for c := 0; c < n; c++ {
			colSum[c] = 0
		}
		for bottom := top; bottom < n; bottom++ {
			for c := 0; c < n; c++ {
				colSum[c] += w[bottom][c]
			}
			pref[0] = 0
			for c := 0; c < n; c++ {
				pref[c+1] = pref[c] + colSum[c]
			}

			// stack 内存放候选起点下标 i，沿栈底到栈顶 pref 值严格递增。
			stack := make([]int, 0, n)
			for j := 0; j < n; j++ {
				i := j // 子数组可以从第 j 列开始
				for len(stack) > 0 && pref[stack[len(stack)-1]] >= pref[i] {
					stack = stack[:len(stack)-1]
				}
				stack = append(stack, i)

				// 子数组 cols[i..j] 的和 = pref[j+1]-pref[i] >= threshold
				// ⇔ pref[i] <= pref[j+1]-threshold；取满足条件的最大 i（最短）。
				bound := pref[j+1] - threshold
				lo, hi := 0, len(stack)
				for lo < hi {
					mid := (lo + hi) / 2
					if pref[stack[mid]] <= bound {
						lo = mid + 1
					} else {
						hi = mid
					}
				}
				if lo > 0 {
					start := stack[lo-1]
					consider(top, start, bottom, j, pref[j+1]-pref[start])
				}
			}
		}
	}
	return best
}
