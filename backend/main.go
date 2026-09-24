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
