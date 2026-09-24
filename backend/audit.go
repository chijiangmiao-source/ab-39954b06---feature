package main

import (
	"errors"
	"time"
)

// point 是 (行, 列) 坐标；复检图变换后的点可能落在画布外（坐标为负或 >= N）。
type point [2]int

type transformResult struct {
	PoseIndex int    `json:"poseIndex"`
	Pose      string `json:"pose"`
	PoseLabel string `json:"poseLabel"`
	Dy        int    `json:"dy"`
	Dx        int    `json:"dx"`
}

// overlayData 是红蓝叠加证据：坐标系与参考图画布一致，
// recheckOnly 中可能包含画布外坐标，由前端扩展视野绘制。
type overlayData struct {
	Matched       []point `json:"matched"`
	ReferenceOnly []point `json:"referenceOnly"`
	RecheckOnly   []point `json:"recheckOnly"`
	RecheckOut    int     `json:"recheckOutOfCanvas"`
}

type auditResponse struct {
	N              int             `json:"n"`
	ReferenceCount int             `json:"referenceCount"`
	RecheckCount   int             `json:"recheckCount"`
	MaxOverlap     int             `json:"maxOverlap"`
	TieCount       int             `json:"tieCount"`
	Transform      transformResult `json:"transform"`
	Overlay        overlayData     `json:"overlay"`
	ElapsedMs      int64           `json:"elapsedMs"`
}

func countOnes(m [][]uint8) int {
	cnt := 0
	for _, row := range m {
		for _, v := range row {
			if v != 0 {
				cnt++
			}
		}
	}
	return cnt
}

// runAudit 执行完整审计：穷举搜索 -> 整数复核规范解 -> 生成叠加证据。
// 任一步异常都返回 error，调用方不得缓存或沿用旧结论。
func runAudit(ref, rec [][]uint8, n int) (*auditResponse, error) {
	start := time.Now()

	res := searchMaxOverlap(ref, rec, n)

	// 纯整数复核规范解，保证 FFT 取整结论与精确计数一致。
	if got := countOverlapDirect(ref, rec, n, res.poseIndex, res.dy, res.dx); got != res.maxOverlap {
		return nil, errors.New("内部校验失败：整数复核与相关结果不一致")
	}

	resp := &auditResponse{
		N:              n,
		ReferenceCount: countOnes(ref),
		RecheckCount:   countOnes(rec),
		MaxOverlap:     res.maxOverlap,
		TieCount:       res.tieCount,
		Transform: transformResult{
			PoseIndex: res.poseIndex,
			Pose:      poses[res.poseIndex].Name,
			PoseLabel: poses[res.poseIndex].Label,
			Dy:        res.dy,
			Dx:        res.dx,
		},
		Overlay: overlayData{
			Matched:       make([]point, 0),
			ReferenceOnly: make([]point, 0),
			RecheckOnly:   make([]point, 0),
		},
	}

	// 用规范变换把复检图缺陷映射回参考图坐标系，生成叠加证据。
	matchedMark := make([]bool, n*n)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(res.poseIndex, r, c, n)
			tr, tc := pr+res.dy, pc+res.dx
			inside := tr >= 0 && tr < n && tc >= 0 && tc < n
			if inside && ref[tr][tc] != 0 {
				resp.Overlay.Matched = append(resp.Overlay.Matched, point{tr, tc})
				matchedMark[tr*n+tc] = true
			} else {
				resp.Overlay.RecheckOnly = append(resp.Overlay.RecheckOnly, point{tr, tc})
				if !inside {
					resp.Overlay.RecheckOut++
				}
			}
		}
	}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if ref[r][c] != 0 && !matchedMark[r*n+c] {
				resp.Overlay.ReferenceOnly = append(resp.Overlay.ReferenceOnly, point{r, c})
			}
		}
	}

	resp.ElapsedMs = time.Since(start).Milliseconds()
	return resp, nil
}
