// Package api 提供坐标卡签发、短码核验与签发记录查询的 HTTP 接口。
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"sars/shortcode"
	"sars/store"
)

const (
	defaultPageSize    = 20
	defaultPageSizeMax = 50
	defaultCursorTTL   = 10 * time.Minute
)

// Options 是记录查询接口的可调参数；零值字段使用默认值。
type Options struct {
	Secret      []byte        // 游标签名密钥；为空则生成随机密钥
	CursorTTL   time.Duration // 快照与游标的有效期；<= 0 时默认 10 分钟
	PageSize    int           // 默认每页数量；<= 0 时默认 20
	PageSizeMax int           // 每页数量上限；<= 0 时默认 50
}

// Server 持有路由所需的依赖。
type Server struct {
	store       *store.Store
	secret      []byte
	cursorTTL   time.Duration
	pageSize    int
	pageSizeMax int
}

// NewRouter 构建 Gin 路由（全部使用默认参数）。
func NewRouter(st *store.Store) *gin.Engine {
	return NewRouterWithOptions(st, Options{})
}

// NewRouterWithOptions 构建 Gin 路由，可覆盖记录查询的分页与游标参数。
func NewRouterWithOptions(st *store.Store, opts Options) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), corsMiddleware())

	s := &Server{
		store:       st,
		secret:      opts.Secret,
		cursorTTL:   opts.CursorTTL,
		pageSize:    opts.PageSize,
		pageSizeMax: opts.PageSizeMax,
	}
	if len(s.secret) == 0 {
		s.secret = newSecret()
	}
	if s.cursorTTL <= 0 {
		s.cursorTTL = defaultCursorTTL
	}
	if s.pageSize <= 0 {
		s.pageSize = defaultPageSize
	}
	if s.pageSizeMax <= 0 {
		s.pageSizeMax = defaultPageSizeMax
	}

	r.GET("/api/health", s.health)
	r.POST("/api/cards", s.issueCard)
	r.GET("/api/cards", s.listCards)
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

// parseLimit 解析每页数量：缺省用默认值，非法值拒绝，超过上限按上限截断。
func (s *Server) parseLimit(raw string) (int, error) {
	n := s.pageSize
	if raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return 0, fmt.Errorf("每页数量无效：须为不超过 %d 的正整数", s.pageSizeMax)
		}
		n = v
	}
	if n > s.pageSizeMax {
		n = s.pageSizeMax
	}
	return n, nil
}

// listCards 按签发时间倒序（时间相同按编号倒序）返回已签发坐标卡，只读。
//
// 首批查询不带 snapshot/cursor：以当前最新记录的时间与编号作为快照边界，
// 冻结本次浏览序列——之后新签发的卡不会插入该序列。翻页时客户端回传
// snapshot 与上一页返回的 next_cursor（不透明、带签名、会过期）做键集分页；
// 伪造、过期或与当前快照不匹配的游标一律拒绝，不泄露任何记录。
func (s *Server) listCards(c *gin.Context) {
	limit, err := s.parseLimit(c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	snapParam := c.Query("snapshot")
	cursorParam := c.Query("cursor")
	if cursorParam != "" && snapParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少快照标识：翻页游标必须与同一次加载的快照一起使用"})
		return
	}

	var snap pageToken
	if snapParam != "" {
		snap, err = s.parseToken(snapParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if snap.Kind != tokenKindSnapshot {
			c.JSON(http.StatusBadRequest, gin.H{"error": errTokenInvalid.Error()})
			return
		}
	} else {
		// 首批查询：确定快照边界（时间与编号）。
		snapTime, snapID, err := s.store.SnapshotBoundary()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取签发记录失败"})
			return
		}
		snap = pageToken{Kind: tokenKindSnapshot, SnapTime: snapTime, SnapID: snapID}
	}

	// 多取一行判断是否还有更早记录。
	q := store.RecordQuery{SnapTime: snap.SnapTime, SnapID: snap.SnapID, Limit: limit + 1}
	if cursorParam != "" {
		cur, err := s.parseToken(cursorParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if cur.Kind != tokenKindCursor {
			c.JSON(http.StatusBadRequest, gin.H{"error": errTokenInvalid.Error()})
			return
		}
		if cur.SnapTime != snap.SnapTime || cur.SnapID != snap.SnapID {
			c.JSON(http.StatusBadRequest, gin.H{"error": errTokenMismatch.Error()})
			return
		}
		q.Paged = true
		q.LastTime = cur.LastTime
		q.LastID = cur.LastID
	}

	cards, err := s.store.ListCards(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取签发记录失败"})
		return
	}

	hasMore := len(cards) > limit
	if hasMore {
		cards = cards[:limit]
	}
	// 每次响应重签快照并顺延有效期：持续翻页的浏览会话不会中途过期。
	exp := time.Now().Add(s.cursorTTL).Unix()
	snap.Exp = exp
	resp := gin.H{
		"cards":       cards,
		"snapshot":    s.signToken(snap),
		"next_cursor": nil,
		"has_more":    hasMore,
	}
	if hasMore && len(cards) > 0 {
		last := cards[len(cards)-1]
		resp["next_cursor"] = s.signToken(pageToken{
			Kind:     tokenKindCursor,
			SnapTime: snap.SnapTime,
			SnapID:   snap.SnapID,
			LastTime: last.IssuedAt.UTC().Format(time.RFC3339),
			LastID:   last.ID,
			Exp:      exp,
		})
	}
	c.JSON(http.StatusOK, resp)
}
