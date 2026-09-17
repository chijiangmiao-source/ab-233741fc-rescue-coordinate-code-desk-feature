// Package api 提供坐标卡签发与短码核验的 HTTP 接口。
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"sars/shortcode"
	"sars/store"
)

// Server 持有路由所需的依赖。
type Server struct {
	store *store.Store
}

// NewRouter 构建 Gin 路由。
func NewRouter(st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), corsMiddleware())

	s := &Server{store: st}
	r.GET("/api/health", s.health)
	r.POST("/api/cards", s.issueCard)
	r.POST("/api/verify", s.verifyCode)
	return r
}

// corsMiddleware 仅方便本地开发直连；Compose 部署时前端经 nginx 反向代理
// /api，不存在跨域。
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// 用指针区分“字段缺失”与“显式为 0”——0 是合法坐标，缺字段才是坏请求。
type issueRequest struct {
	X *int `json:"x"`
	Y *int `json:"y"`
}

// issueCard 校验坐标、按判据计算短码，并把成功签发的坐标卡落库。
func (s *Server) issueCard(c *gin.Context) {
	var req issueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体无效：需要 JSON 整数字段 x、y（各为 0 至 9999）"})
		return
	}
	if req.X == nil || req.Y == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少坐标字段：x、y 都必填"})
		return
	}
	code, err := shortcode.Encode(*req.X, *req.Y)
	if errors.Is(err, shortcode.ErrRange) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "短码编码失败"})
		return
	}
	card, err := s.store.SaveCard(*req.X, *req.Y, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "坐标卡落库失败"})
		return
	}
	c.JSON(http.StatusCreated, card)
}

type verifyRequest struct {
	Code string `json:"code"`
}

// verifyCode 核验短码。格式或校验失败时返回 422，绝不还原坐标、绝不落库；
// 核验通过时还原唯一坐标，并附带本台是否签发过该码的记录。
func (s *Server) verifyCode(c *gin.Context) {
	var req verifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体无效：需要 JSON 字符串字段 code"})
		return
	}
	x, y, err := shortcode.Decode(req.Code)
	if errors.Is(err, shortcode.ErrFormat) || errors.Is(err, shortcode.ErrChecksum) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"valid": false, "error": err.Error()})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "短码核验失败"})
		return
	}
	resp := gin.H{"valid": true, "x": x, "y": y, "code": req.Code, "issued": false}
	if card, found, err := s.store.FindByCode(req.Code); err == nil && found {
		resp["issued"] = true
		resp["issued_at"] = card.IssuedAt
	}
	c.JSON(http.StatusOK, resp)
}
