package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

type auditRequest struct {
	Reference string `json:"reference"`
	Recheck   string `json:"recheck"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	// 开发期跨域放开；容器部署时前端经 nginx 同源代理 /api，不依赖 CORS。
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.POST("/api/audit", handleAudit)
	r.POST("/api/refute", handleRefute)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("backend listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

func handleAudit(c *gin.Context) {
	// 512x512 的文本约 262KB/图，16MB 上限足够且防止滥用。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)

	var req auditRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInputError(c, &InputError{Message: "请求体不是合法 JSON 或超出大小限制"})
		return
	}

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

	resp, err := runAudit(ref, rec, n)
	if err != nil {
		// 计算异常：返回 500 且不写任何结论字段，前端同步清空旧结论。
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func writeInputError(c *gin.Context, e *InputError) {
	c.JSON(http.StatusBadRequest, gin.H{"error": e})
}

// handleRefute 处理最小遮挡反证复核请求。
// 与 /api/audit 一样从两幅原图重新解析、重新做既有精确配准，
// 绝不信任浏览器保存的最大重合或变换；候选校验顺序固定为 姿态 -> 纵移 -> 横移。
func handleRefute(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)

	var req refuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInputError(c, &InputError{Message: "请求体不是合法 JSON 或超出大小限制"})
		return
	}

	// 第一步与 /api/audit 完全一致：从两幅原图重新解析，不信任任何浏览器侧缓存。
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
	if n > refuteMaxN {
		writeInputError(c, &InputError{
			Field:   "reference",
			Message: fmt.Sprintf("反证复核入口仅支持边长 %d..%d（主审计支持至 %d，不变）", minN, refuteMaxN, maxN),
		})
		return
	}

	// 第二步按既有顺序校验目标候选：姿态、纵移、横移，逐项报告首个错误。
	if req.PoseIndex == nil {
		writeInputError(c, &InputError{Field: "poseIndex", Message: "缺少非规范姿态 poseIndex（0..7 的整数，按 D4 固定顺序）"})
		return
	}
	if *req.PoseIndex < 0 || *req.PoseIndex > 7 {
		writeInputError(c, &InputError{
			Field:   "poseIndex",
			Message: fmt.Sprintf("非规范姿态下标 %d 越界，需为 0..7 的整数", *req.PoseIndex),
		})
		return
	}
	if req.Dy == nil {
		writeInputError(c, &InputError{Field: "dy", Message: "缺少纵移 dy（-(N-1)..(N-1) 的整数）"})
		return
	}
	if *req.Dy < -(n-1) || *req.Dy > n-1 {
		writeInputError(c, &InputError{
			Field:   "dy",
			Message: fmt.Sprintf("纵移 %d 超出范围 [-(N-1), N-1] = [%d, %d]", *req.Dy, -(n - 1), n-1),
		})
		return
	}
	if req.Dx == nil {
		writeInputError(c, &InputError{Field: "dx", Message: "缺少横移 dx（-(N-1)..(N-1) 的整数）"})
		return
	}
	if *req.Dx < -(n-1) || *req.Dx > n-1 {
		writeInputError(c, &InputError{
			Field:   "dx",
			Message: fmt.Sprintf("横移 %d 超出范围 [-(N-1), N-1] = [%d, %d]", *req.Dx, -(n - 1), n-1),
		})
		return
	}

	resp, err := runRefute(ref, rec, n, *req.PoseIndex, *req.Dy, *req.Dx)
	if err != nil {
		// 计算异常：500 且不携带任何结论/窗口字段，前端清空复核证据。
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}
