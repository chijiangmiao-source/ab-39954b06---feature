package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// counterMaxN 限定最小遮挡反证复核只支持边长 16..96 的输入
// （/api/audit 本身仍支持 16..512，二者互不影响）。
const counterMaxN = 96

// counterWindow 是命中差权值网格上的轴对齐非空矩形（坐标均为画布坐标，含端点）。
type counterWindow struct {
	Top              int     `json:"top"`
	Left             int     `json:"left"`
	Bottom           int     `json:"bottom"`
	Right            int     `json:"right"`
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	Area             int     `json:"area"`
	RemovedCanonical int     `json:"removedCanonical"`
	RemovedTarget    int     `json:"removedTarget"`
	RemovedPoints    []point `json:"removedPoints"`
	CanonicalAfter   int     `json:"canonicalOverlapAfter"`
	TargetAfter      int     `json:"targetOverlapAfter"`
}

// counterCandidate 回显一个候选（规范候选或目标候选）及其精确重合数。
type counterCandidate struct {
	transformResult
	Overlap int `json:"overlap"`
}

// counterResponse 是最小遮挡反证复核结论。
// 不可反证时 Feasible=false 且 Window 为 nil，绝不伪造矩形。
type counterResponse struct {
	N         int              `json:"n"`
	Feasible  bool             `json:"feasible"`
	Canonical counterCandidate `json:"canonical"`
	Target    counterCandidate `json:"target"`
	Gap       int              `json:"gap"`
	Window    *counterWindow   `json:"window"`
}

type counterRequest struct {
	Reference string `json:"reference"`
	Recheck   string `json:"recheck"`
	// 指针用于区分“缺字段”与“显式传 0”。
	TargetPose *int `json:"targetPose"`
	TargetDy   *int `json:"targetDy"`
	TargetDx   *int `json:"targetDx"`
}

func handleCounterEvidence(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)

	var req counterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInputError(c, &InputError{Message: "请求体不是合法 JSON、字段类型非整数或超出大小限制"})
		return
	}

	// 必须重新解析两幅原图：浏览器侧保存的最大重合数与变换一律不可信。
	ref, n, ierr := parseMatrix("reference", req.Reference)
	if ierr != nil {
		writeInputError(c, ierr)
		return
	}
	rec, n2, ierr := parseMatrix("recheck", req.Recheck)
	if ierr != nil {
		writeInputError(c, ierr)
		return
	}
	if n != n2 {
		writeInputError(c, &InputError{
			Field:   "recheck",
			Message: fmt.Sprintf("两图边长不一致：参考图 N=%d，复检图 N=%d", n, n2),
		})
		return
	}
	if n > counterMaxN {
		writeInputError(c, &InputError{
			Field:   "reference",
			Message: fmt.Sprintf("最小遮挡反证复核仅支持边长 16..%d，当前边长 N=%d", counterMaxN, n),
		})
		return
	}

	// 按既有姿态、纵移、横移的顺序校验目标候选。
	if req.TargetPose == nil {
		writeInputError(c, &InputError{Field: "targetPose", Message: "缺少目标姿态 targetPose"})
		return
	}
	pose := *req.TargetPose
	if pose < 0 || pose >= len(poses) {
		writeInputError(c, &InputError{
			Field:   "targetPose",
			Message: fmt.Sprintf("目标姿态须为 0..7 的整数，收到 %d", pose),
		})
		return
	}
	if req.TargetDy == nil {
		writeInputError(c, &InputError{Field: "targetDy", Message: "缺少目标纵移 targetDy"})
		return
	}
	dy := *req.TargetDy
	if dy < -(n-1) || dy > n-1 {
		writeInputError(c, &InputError{
			Field:   "targetDy",
			Message: fmt.Sprintf("目标纵移须在 [%d, %d] 内，收到 %d", -(n - 1), n-1, dy),
		})
		return
	}
	if req.TargetDx == nil {
		writeInputError(c, &InputError{Field: "targetDx", Message: "缺少目标横移 targetDx"})
		return
	}
	dx := *req.TargetDx
	if dx < -(n-1) || dx > n-1 {
		writeInputError(c, &InputError{
			Field:   "targetDx",
			Message: fmt.Sprintf("目标横移须在 [%d, %d] 内，收到 %d", -(n - 1), n-1, dx),
		})
		return
	}

	resp, err := runCounterEvidence(ref, rec, n, pose, dy, dx)
	if err != nil {
		if ie, ok := err.(*InputError); ok {
			writeInputError(c, ie)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// runCounterEvidence 重新完成既有精确配准取得规范候选，再与目标候选逐点比较，
// 精确求解最小遮挡反证窗口。
func runCounterEvidence(ref, rec [][]uint8, n, pose, dy, dx int) (*counterResponse, error) {
	// 既有精确配准：与 /api/audit 完全一致地重跑穷举相关搜索，不信任任何外部结果。
	res := searchMaxOverlap(ref, rec, n)
	if got := countOverlapDirect(ref, rec, n, res.poseIndex, res.dy, res.dx); got != res.maxOverlap {
		return nil, fmt.Errorf("内部校验失败：整数复核与相关结果不一致")
	}
	if pose == res.poseIndex && dy == res.dy && dx == res.dx {
		return nil, &InputError{
			Field:   "targetPose",
			Message: "目标候选与规范候选（姿态、纵移、横移）完全相同，不构成反证候选",
		}
	}

	canonicalOverlap := res.maxOverlap
	targetOverlap := countOverlapDirect(ref, rec, n, pose, dy, dx)

	// 标记参考缺陷集合与两个候选各自命中的参考缺陷。
	refMark := make([]bool, n*n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if ref[r][c] != 0 {
				refMark[r*n+c] = true
			}
		}
	}
	markHits := func(p, ty, tx int) []bool {
		hit := make([]bool, n*n)
		for r := 0; r < n; r++ {
			for c := 0; c < n; c++ {
				if rec[r][c] == 0 {
					continue
				}
				pr, pc := applyPose(p, r, c, n)
				tr, tc := pr+ty, pc+tx
				if tr >= 0 && tr < n && tc >= 0 && tc < n && refMark[tr*n+tc] {
					hit[tr*n+tc] = true
				}
			}
		}
		return hit
	}
	canonicalHit := markHits(res.poseIndex, res.dy, res.dx)
	targetHit := markHits(pose, dy, dx)

	// 每个参考缺陷的命中差：规范命中+1、目标命中-1，取值 -1/0/1。
	weights := make([]int, n*n)
	for i := range weights {
		if !refMark[i] {
			continue
		}
		v := 0
		if canonicalHit[i] {
			v++
		}
		if targetHit[i] {
			v--
		}
		weights[i] = v
	}

	gap := canonicalOverlap - targetOverlap
	resp := &counterResponse{
		N:   n,
		Gap: gap,
		Canonical: counterCandidate{
			transformResult: transformResult{
				PoseIndex: res.poseIndex,
				Pose:      poses[res.poseIndex].Name,
				PoseLabel: poses[res.poseIndex].Label,
				Dy:        res.dy,
				Dx:        res.dx,
			},
			Overlap: canonicalOverlap,
		},
		Target: counterCandidate{
			transformResult: transformResult{
				PoseIndex: pose,
				Pose:      poses[pose].Name,
				PoseLabel: poses[pose].Label,
				Dy:        dy,
				Dx:        dx,
			},
			Overlap: targetOverlap,
		},
	}

	// 移除窗口内参考缺陷后目标候选严格胜过规范候选：
	//   T - sum_t(W 窗内 t 命中数) > C - sum_c
	//   ⟺ 窗内权值和 (cHit-tHit) > C-T = gap
	//   ⟺ 窗内权值和 >= gap+1（整数）。
	threshold := gap + 1
	top, left, bottom, right, ok := minWeightRectangle(weights, n, threshold)
	if !ok {
		// 不可反证：如实返回，Window 保持 nil。
		return resp, nil
	}

	w := &counterWindow{
		Top: top, Left: left, Bottom: bottom, Right: right,
		Width: right - left + 1, Height: bottom - top + 1,
		RemovedPoints: make([]point, 0),
	}
	w.Area = w.Width * w.Height
	sumW := 0
	for r := top; r <= bottom; r++ {
		for c := left; c <= right; c++ {
			i := r*n + c
			if !refMark[i] {
				continue
			}
			sumW += weights[i]
			w.RemovedPoints = append(w.RemovedPoints, point{r, c})
			if canonicalHit[i] {
				w.RemovedCanonical++
			}
			if targetHit[i] {
				w.RemovedTarget++
			}
		}
	}
	if sumW < threshold {
		return nil, fmt.Errorf("内部校验失败：最小窗口权值和 %d 未达阈值 %d", sumW, threshold)
	}
	w.CanonicalAfter = canonicalOverlap - w.RemovedCanonical
	w.TargetAfter = targetOverlap - w.RemovedTarget
	if !(w.TargetAfter > w.CanonicalAfter) {
		return nil, fmt.Errorf("内部校验失败：移除后目标候选仍未严格胜出")
	}

	resp.Feasible = true
	resp.Window = w
	return resp, nil
}

// minWeightRectangle 在 n×n 权值网格（含负权）的所有轴对齐非空矩形中，
// 精确找出权值和 >= threshold 且面积最小者；同面积按 (上, 左, 下, 右) 升序裁决。
// 返回 ok=false 表示不存在这样的矩形，调用方必须按不可反证处理。
//
// 这里不能用双指针贪心滑窗：权值含负数时前缀和不单调，
// “伸右端、缩左端”的滑窗会漏掉可行矩形。做法是枚举上下边界（O(n²)），
// 把区间压成一维列和，再对每个右端点 e 在其左侧前缀中线性查找
// 满足 P[s] <= P[e]-threshold 的最大 s（即最短可行区间）；
// 负权由前缀差精确表达，不依赖任何单调性。
func minWeightRectangle(w []int, n, threshold int) (top, left, bottom, right int, ok bool) {
	bestArea := n*n + 1
	var best [4]int
	consider := func(t, l, b, r int) {
		area := (b - t + 1) * (r - l + 1)
		cand := [4]int{t, l, b, r}
		if !ok || area < bestArea ||
			(area == bestArea && lexLess(cand, best)) {
			ok = true
			bestArea = area
			best = cand
		}
	}

	colSum := make([]int, n)
	prefix := make([]int, n+1)
	for t := 0; t < n; t++ {
		for i := range colSum {
			colSum[i] = 0
		}
		for b := t; b < n; b++ {
			// 高度本身已超过已知最优面积（最小宽度为 1），继续加底边只会更大。
			if ok && (b-t+1) > bestArea {
				break
			}
			rowOff := b * n
			for c := 0; c < n; c++ {
				colSum[c] += w[rowOff+c]
			}
			prefix[0] = 0
			for c := 0; c < n; c++ {
				prefix[c+1] = prefix[c] + colSum[c]
			}
			height := b - t + 1
			for e := 1; e <= n; e++ {
				bound := prefix[e] - threshold
				// 从 e-1 向左找最大的 s：固定右端点下区间最短。
				for s := e - 1; s >= 0; s-- {
					if prefix[s] <= bound {
						// 面积不可能更优时仍需调用以参与同面积坐标裁决。
						width := e - s
						if height*width <= bestArea {
							consider(t, s, b, e-1)
						}
						break // s 已最大，同右端点无更短区间
					}
				}
			}
		}
	}
	if !ok {
		return 0, 0, 0, 0, false
	}
	return best[0], best[1], best[2], best[3], true
}

// lexLess 按 (上, 左, 下, 右) 顺序比较两个矩形坐标。
func lexLess(a, b [4]int) bool {
	for i := 0; i < 4; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
