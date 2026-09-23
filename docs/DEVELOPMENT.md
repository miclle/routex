# 本地开发与数据库测试

Go 与 Vite 在宿主机运行，保留现有热重载和调试体验；Docker Compose 管理数据库。需要 Go、Node.js、Docker Engine 与 Docker Compose。数据库镜像为 PostgreSQL 18 和 MySQL 8.4。

## 启动 PostgreSQL 开发环境

```bash
go tool task install
go tool task db:up
go tool task dev
```

`db:up` 等待数据库健康后返回。首次 `dev` 从示例创建 `cmd/routex/config.local.yaml`；已有配置不会被覆盖。如果本地配置仍使用旧端口，按下表修改，或设置 `DATABASE_URL`（本地配置的 `dsn` 需使用示例中的环境变量写法）。

| 配置 | PostgreSQL（默认） | MySQL（可选） |
|---|---|---|
| 启动命令 | `go tool task db:up` | `go tool task db:mysql` |
| 地址 | `127.0.0.1:15433` | `127.0.0.1:13306` |
| 数据库 | `routex` | `routex` |
| 用户 | `routex` | `routex` |
| 开发密码 | `routex-local` | `routex-local` |
| Compose profile | 默认 | `mysql` |

这些固定凭证仅供本地开发。数据库端口只绑定 loopback。端口冲突时可以通过 `ROUTEX_POSTGRES_PORT` 或 `ROUTEX_MYSQL_PORT` 修改 Compose 映射，并同步设置应用 `DATABASE_URL`；端口变量不会自动修改已有应用配置。

```bash
# 使用 MySQL；需使用支持这些环境变量的 config.local.yaml。
go tool task db:mysql
ROUTEX_DB_DRIVER=mysql \
DATABASE_URL='routex:routex-local@tcp(127.0.0.1:13306)/routex?charset=utf8mb4&parseTime=True&loc=UTC' \
go tool task dev
```

应用默认监听 9000，Vite 默认监听 5173，可分别用 `ROUTEX_HTTP_PORT` 和 `ROUTEX_VITE_PORT` 调整。开发服务不会扫描或终止其他项目进程。

## 停止、状态与数据保留

```bash
go tool task db:status
go tool task db:down
```

`db:down` 停止并移除本项目容器及网络，保留 PostgreSQL 和 MySQL 命名卷。再次启动会保留已有数据。日常操作不要添加 `--volumes` / `-v`，这会删除开发数据库；修改镜像主版本也需要先备份并按数据库升级流程迁移。多个 checkout 同时开发时，为各自设置不同的 `COMPOSE_PROJECT_NAME` 和宿主端口。

## 自动化测试

```bash
go tool task check
go tool task test
go tool task test-integration
go tool actionlint
```

`test` 运行 Go race、前端行为、开发进程生命周期及生产资产测试。`test-integration` 单独启动 `compose.test.yaml` 中的 PostgreSQL 和 MySQL，执行未缓存的 Go race 测试，然后清理本次测试容器与网络；CI 也执行相同入口。测试使用唯一 Compose 项目、随机 loopback 端口和内存临时存储，不读取或修改开发数据库命名卷。失败时输出测试数据库容器日志，清理失败会使命令失败。

脚本将测试地址传入以下环境变量：

- `ROUTEX_TEST_POSTGRES_DSN`
- `ROUTEX_TEST_MYSQL_DSN`

普通 `go test` 未配置变量时跳过需要数据库的测试。手动提供变量时必须使用专用测试数据库，测试会写入数据；不得指向开发或生产数据库。集成脚本自行生成 DSN，不沿用调用者的同名环境变量。

数据库集成测试证明当前已实现的迁移和业务行为；尚未实现的网关、额度及外部系统不由此视为通过验收。
