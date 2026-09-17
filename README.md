# 山地搜救坐标短码

指挥席把地图坐标口述给前队时，用九字符短码代替两组长数字：短码自带模 11 校验符，**任何单字符抄错都会被当场拒绝**，不会把队员引向错误山坳。

- `web/` — Vue 3 签发页与核验页（Vite 构建，nginx 托管并反代 `/api`）
- `server/` — Gin API，SQLite 落库成功签发的坐标卡
- `verify/` — 一次性验收服务（固定合法/篡改样例跑真实服务）
- 前后端的短码判据是同一套规则：`server/shortcode/shortcode.go` 与 `web/src/shortcode.js` 逐条对应，并用**同一组固定样例**测试

## 操作说明

```bash
# 启动前端 + API（默认 WEB_PORT=8080、API_PORT=8081）
docker compose up -d

# 覆盖宿主端口
WEB_PORT=9000 API_PORT=9001 docker compose up -d

# 跑一次性验收服务（退出码 0 即全部通过）
docker compose up --exit-code-from verify verify   # 或 docker compose run --rm verify

# 打开页面
#   签发席  http://localhost:8080/#/issue
#   记录席  http://localhost:8080/#/records
#   核验席  http://localhost:8080/#/verify
```

### 编码示例（与操作对照）

| 操作 | 输入 | 结果 |
| --- | --- | --- |
| 签发 | `x=1234, y=5678` | 短码 **`152637483`** |
| 签发 | `x=3, y=0` | 短码 **`00000030X`**（余数十写作 X） |
| 签发 | `x=0, y=0` | 短码 **`000000000`** |
| 签发 | `x=9999, y=9999` | 短码 **`999999995`** |
| 核验 | `152637483` | 通过，唯一坐标 `(1234, 5678)` |
| 核验 | `152637493`（第 7 位被篡改） | **拒绝**：校验符不匹配，不还原坐标 |
| 核验 | `15263748`（少一位） | **拒绝**：格式无效 |
| 签发 | `x=10000` 或 `x=-1` | **拒绝**：坐标须为 0–9999 的整数米 |

以 `x=1234, y=5678` 为例，手工复算一遍：

1. 各补足四位：`x=1234`、`y=5678`；
2. 按 x 千位、y 千位、x 百位、y 百位……交错：`1 5 2 6 3 7 4 8`；
3. 从左到右乘 1–8 求和：`1+10+6+24+15+42+28+64 = 190`；
4. `190 ÷ 11` 余 `3`，校验符为 `3`；
5. 短码 = `15263748` + `3` = **`152637483`**。

## 短码判据（前后端同一套）

- 输入只接受 `x`、`y` 各为 **0 至 9999 的十进制整数米**，不做单位换算或坐标归一化；
- `x`、`y` 各补足四位后，按 **x 千位、y 千位、x 百位、y 百位……直至个位**交错，得到八位主体；
- 主体从左到右八个数字分别乘 **1 至 8**，求和除以 **11** 的余数作为校验符，**余数十写作 X**，其余写对应数字；
- 最终短码**固定九字符**：八位主体 + 一位校验符；
- 核验只接受 `^[0-9]{8}[0-9X]$`；校验失败**不还原坐标、不落库**；
- 因为 11 是素数且权重 1–8 与增量 ±1–±9 都非零模 11，**任意单字符篡改必然改变余数**，必被拒绝。

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/health` | 健康检查 |
| `POST` | `/api/cards` | 签发：`{"x":1234,"y":5678}` → `201 {id,x,y,code,issued_at}`，成功即落库；坐标非法 → `400` |
| `POST` | `/api/verify` | 核验：`{"code":"152637483"}` → `200 {valid,x,y,code,issued,issued_at?}`；格式/校验失败 → `422`，不还原、不落库 |
| `GET` | `/api/cards` | 签发记录（只读）：`?limit=&snapshot=&cursor=` → `200 {cards,snapshot,next_cursor,has_more}`；非法/过期/不匹配游标 → `400` |

### 签发记录的分页约定

- 首批查询不带参数：以当时最新记录的**签发时间与编号**作为快照边界，本次浏览序列随即冻结——翻页期间新签发的卡不会插入该序列，也不会造成重复或遗漏；
- 之后每页回传上一页响应里的 `snapshot` 与 `next_cursor`（不透明、HMAC 签名、默认 10 分钟过期，可用 `CURSOR_SECRET` / `CURSOR_TTL_SECONDS` 配置）做键集分页；
- `limit` 缺省 20、上限 50，非法值 `400`；伪造、过期或与当前快照不匹配的游标一律 `400`，不泄露任何记录；
- 记录页可把选中的短码带入核验页（`/#/verify?code=…`），只预填、不自动提交，核验仍走真实核验链路。

## 测试

三处测试使用**同一组固定合法样例与篡改样例**，错误反馈全部来自真实判据与真实 API，不打桩、不用固定结果：

```bash
# 后端：testify（短码判据、HTTP 接口、SQLite 落库）
cd server && go test ./...

# 前端单元：Vitest（与 Go 相同的样例，含单字符篡改穷举）
cd web && npm install && npm test

# 端到端：Playwright（需先 docker compose up -d，打真实服务）
cd web && npx playwright install chromium && npm run test:e2e

# 一次性验收服务（容器内跑真实栈）
docker compose up verify
```

## 本地开发

```bash
# API（默认 :8080，可用 PORT / DB_PATH / CURSOR_SECRET / CURSOR_TTL_SECONDS 覆盖）
cd server && go run .

# 前端（:5173，/api 代理到 API_PORT，默认 8081）
cd web && npm install && API_PORT=8080 npm run dev
```

## 目录结构

```
├── docker-compose.yml      # web + api + verify（一次性验收）
├── server/                 # Gin API
│   ├── shortcode/          # 短码判据（Go 实现 + testify）
│   ├── store/              # SQLite 坐标卡落库与记录键集分页
│   └── api/                # HTTP 路由与 handler（含游标签名）
├── web/                    # Vue 3 前端
│   ├── src/shortcode.js    # 短码判据（JS 实现，与 Go 同一套）
│   ├── src/views/          # 签发页 / 记录页 / 核验页
│   └── e2e/                # Playwright（固定合法/篡改样例 + 记录翻页）
└── verify/                 # 一次性验收服务
```
