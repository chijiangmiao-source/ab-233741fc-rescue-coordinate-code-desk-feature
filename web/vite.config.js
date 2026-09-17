import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

// 开发服务器把 /api 代理到本机 API 端口（与 docker-compose 的 API_PORT 一致）。
export default defineConfig({
  plugins: [vue()],
  server: {
    host: true, // 监听所有网卡，避免 localhost 仅解析到单一协议栈时连不上
    port: 5173,
    proxy: {
      '/api': `http://localhost:${process.env.API_PORT || 8081}`,
    },
  },
  test: {
    include: ['src/**/*.test.js'],
  },
});
