// Package api 提供坐标卡签发、短码核验与签发记录只读查询的 HTTP 接口。
package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"sars/shortcode"
	"sars/store"
)

// 签发记录分页参数：默认每页 20 条，单页最多 50 条，防止一次拉取过量数据。
const (
	defaultPageSize = 20
	maxPageSize     = 50
)

// Server 持有路由所需的依赖。
type Server struct {
	store  *store.Store
	cursor *cursorSigner
}

// NewRouter 构建 Gin 路由。
func NewRouter(st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), corsMiddleware())

	signer, err := newCursorSigner()
	if err != nil {
		// 密钥只在启动时生成一次；失败属于配置/系统错误，直接 panic 由
		// 进程管理器重启，好过带病提供可被伪造游标攻击的接口。
		panic(err)
	}
	s := &Server{store: st, cursor: signer}
	return newRouter(s)
}

// newRouter 在给定 Server（含游标签名器）上注册路由，便于测试用自定义
// 签名器或时钟构造完整路由。
func newRouter(s *Server) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), corsMiddleware())

	r.GET("/api/health", s.health)
	r.POST("/api/cards", s.issueCard)
	// 只读列表必须 no-store：首次与重开记录是同一个无游标 URL，
	// 缺少缓存指令时浏览器会回退到启发式缓存，把旧快照当成新快照。
	r.GET("/api/cards", noStoreMiddleware(), s.listCards)
	r.POST("/api/verify", s.verifyCode)
	return r
}

// noStoreMiddleware 要求浏览器与中间代理一律不缓存响应。
func noStoreMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
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

// issuedAtText 把时间格式化为与库中一致的 RFC3339（UTC，秒精度）。
func issuedAtText(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// listCards 是只读的签发记录查询：按签发时间倒序键集分页。
//
// 首次请求不带 cursor，服务端以当时最新一张卡确定快照边界并签名进游标；
// 之后每页回传上一页的 next_cursor。快照期间新签发的卡编号更大，被
// id <= 快照边界挡在当前序列之外，既不会插队也不会导致重复或遗漏。
// 伪造、过期或与当前快照不匹配的游标一律 400 拒绝，响应体不含任何记录。
func (s *Server) listCards(c *gin.Context) {
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token := c.Query("cursor")
	if token == "" {
		cards, snap, hasMore, err := s.store.PageFirst(limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发记录查询失败"})
			return
		}
		resp := gin.H{"cards": cards, "has_more": hasMore, "next_cursor": ""}
		if hasMore {
			last := cards[len(cards)-1]
			resp["snapshot_id"] = snap.BoundaryID
			resp["next_cursor"] = s.cursor.sign(snap.BoundaryID, snap.IssuedAt, last.ID)
		} else if len(cards) > 0 {
			// 单页就到底：回传快照编号便于前端后续按同一快照重试，
			// 但没有下一页游标。
			resp["snapshot_id"] = snap.BoundaryID
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	p, err := s.cursor.parse(token)
	if errors.Is(err, errCursorExpired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "cursor_expired"})
		return
	} else if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "游标无效，无法继续查询", "code": "cursor_invalid"})
		return
	}

	// 与当前快照不匹配的游标：快照边界卡必须仍在且签发时间未漂移。
	// 边界卡只可能因人工删库消失，此时明确拒绝，绝不退化成从头查询
	// （那样会把浏览期间新签发的卡混进序列）。
	boundary, found, err := s.store.CardAt(p.SnapID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发记录查询失败"})
		return
	}
	if !found || issuedAtText(boundary.IssuedAt) != p.SnapAt {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "游标与当前签发记录快照不匹配，请重新打开签发记录",
			"code":  "cursor_stale",
		})
		return
	}

	// 锚点卡（上一页最后一条）必须仍在且属于同一快照，防止游标被挪用到
	// 别的序列或指向不存在的键集位置。
	anchor, found, err := s.store.CardAt(p.AfterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发记录查询失败"})
		return
	}
	if !found || anchor.ID > p.SnapID || issuedAtText(anchor.IssuedAt) > p.SnapAt {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "游标与当前签发记录快照不匹配，请重新打开签发记录",
			"code":  "cursor_stale",
		})
		return
	}

	cards, hasMore, err := s.store.PageAfter(p.SnapID, issuedAtText(anchor.IssuedAt), anchor.ID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发记录查询失败"})
		return
	}
	resp := gin.H{
		"cards":       cards,
		"has_more":    hasMore,
		"snapshot_id": p.SnapID,
		"next_cursor": "",
	}
	if hasMore {
		last := cards[len(cards)-1]
		resp["next_cursor"] = s.cursor.sign(p.SnapID, p.SnapAt, last.ID)
	}
	c.JSON(http.StatusOK, resp)
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
