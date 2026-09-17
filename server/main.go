// 山地搜救坐标短码服务：签发坐标卡、核验短码、查询签发记录。
package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"sars/api"
	"sars/store"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	st, err := store.Open(envOr("DB_PATH", "sars.db"))
	if err != nil {
		log.Fatalf("无法打开数据库: %v", err)
	}
	defer st.Close()

	// 记录查询的游标参数：密钥可用 CURSOR_SECRET 固定（默认随机生成，
	// 重启后旧游标失效），有效期可用 CURSOR_TTL_SECONDS 覆盖（默认 600 秒）。
	var opts api.Options
	if v := os.Getenv("CURSOR_SECRET"); v != "" {
		opts.Secret = []byte(v)
	}
	if v := os.Getenv("CURSOR_TTL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.CursorTTL = time.Duration(n) * time.Second
		} else {
			log.Printf("忽略非法的 CURSOR_TTL_SECONDS=%q", v)
		}
	}

	addr := ":" + envOr("PORT", "8080")
	log.Printf("坐标短码服务监听 %s", addr)
	if err := api.NewRouterWithOptions(st, opts).Run(addr); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}
