<div align="center">

![new-api](/web/public/logo.png)

# New API

🍥 新一代大模型网关与 AI 资产管理系统

</div>

---

## 📝 项目简介

New API 是基于 [One API](https://github.com/songquanpeng/one-api) 二次开发的 LLM 网关与 AI 资产管理系统，聚合多家上游 AI 服务，提供统一的 API 接口、令牌与额度管理、计费统计以及可视化控制台。

> [!IMPORTANT]
> 本项目仅用于合法、获授权的 AI API 网关、组织级鉴权、多模型管理、用量统计与私有化部署场景。使用者需合法获取上游 API 密钥与服务权限，并遵守上游服务条款及相关法律法规。

## ✨ 主要特性

- 🎨 现代化 UI，多语言支持
- 🔄 完全兼容原 One API 数据库
- 📈 可视化控制台与用量统计
- 🔒 令牌分组、模型限制、用户管理
- 💰 按次 / 按量 / 缓存命中计费
- 🔀 多种 API 格式支持与互转（OpenAI / Claude / Gemini 等）
- ⚖️ 渠道加权、失败自动重试、用户级限流

> 更多功能详见[官方文档](https://docs.newapi.pro/docs)。

## 🚀 部署

镜像发布在私有阿里云容器镜像仓库：`registry.cn-hongkong.aliyuncs.com/catalyst_clan/new-api`。

### 部署要求

| 组件 | 要求 |
|------|------|
| 本地数据库 | SQLite（需挂载 `/data` 目录）|
| 远程数据库 | MySQL ≥ 5.7.8 或 PostgreSQL ≥ 9.6 |
| 容器引擎 | Docker / Docker Compose |
| 系统架构 | 仅支持 64 位（amd64 / arm64）|

### 登录私有仓库

镜像为私有仓库，首次部署需先登录：

```bash
docker login registry.cn-hongkong.aliyuncs.com
```

### 方式一：Docker Compose（推荐）

```bash
git clone https://github.com/QuantumNous/new-api.git
cd new-api

# 按需编辑 docker-compose.yml（数据库、密码等）
docker compose up -d
```

### 方式二：Docker 命令

使用 SQLite：

```bash
docker run --name new-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  registry.cn-hongkong.aliyuncs.com/catalyst_clan/new-api:latest
```

使用 MySQL：

```bash
docker run --name new-api -d --restart always \
  -p 3000:3000 \
  -e SQL_DSN="root:123456@tcp(localhost:3306)/oneapi" \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  registry.cn-hongkong.aliyuncs.com/catalyst_clan/new-api:latest
```

> 💡 `-v ./data:/data` 会将数据保存在当前目录的 `data` 文件夹，也可改为绝对路径，如 `-v /your/path:/data`。

部署完成后访问 `http://localhost:3000` 即可开始使用。

### 常用环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `SESSION_SECRET` | 鉴权签名密钥，多机部署时所有节点必须一致 | - |
| `SQL_DSN` | 数据库连接字符串 | - |
| `REDIS_CONN_STRING` | Redis 连接字符串 | - |
| `STREAMING_TIMEOUT` | 流式超时（秒）| `300` |
| `TZ` | 时区 | `Asia/Shanghai` |

> 完整配置见[环境变量文档](https://docs.newapi.pro/docs/installation/config-maintenance/environment-variables)。

> [!WARNING]
> 多机部署时，所有节点必须使用同一主数据库与相同的 `SESSION_SECRET`；共享 Redis 的节点还需使用相同的 `CRYPTO_SECRET`，否则鉴权与缓存无法一致。

## 📜 开源协议

本项目基于 [GNU AGPLv3](./LICENSE) 开源。

依据 AGPLv3 第 7 条附加条款：修改版本必须在相应法律声明，以及界面中显著的关于、法律、页脚或署名位置，保留作者署名 `Frontend design and development by New API contributors.`，并保留指向原始项目的可见链接：<https://github.com/QuantumNous/new-api>。

本项目基于 [One API](https://github.com/songquanpeng/one-api)（MIT 协议）二次开发。

---

<div align="center">

<sub>Built with ❤️ by QuantumNous</sub>

</div>
