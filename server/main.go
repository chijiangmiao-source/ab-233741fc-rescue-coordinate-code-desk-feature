// 山地搜救坐标短码服务：签发坐标卡、核验短码。
package main

import (
	"log"
	"os"

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

	addr := ":" + envOr("PORT", "8080")
	log.Printf("坐标短码服务监听 %s", addr)
	if err := api.NewRouter(st).Run(addr); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}
