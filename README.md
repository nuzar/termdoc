# termdoc

`termdoc` 是一个用 Go 写的**离线编程文档查询工具**，目标是做 devdocs.io / Zeal / Dash 的终端替代品。它把文档预先下载或导入到本地，之后在完全断网、只有 SSH 终端的环境里按 tldr 式工作流使用：

```console
$ termdoc search println
  fmt.Println    Function  go
  log.Println    Function  go

$ termdoc show fmt.Println
```

- 单个静态二进制，`CGO_ENABLED=0` 可编译，无运行时依赖。
- 支持两类文档源：**devdocs.io** 和 **Zeal/Dash**（含用户贡献的 docset）。
- 搜索索引只索引条目名称与类型（与 Zeal/Dash 行为一致），支持前缀与 camelCase 子词匹配。
- 文档正文从 HTML 提取，转成 Markdown 后用 [glamour](https://github.com/charmbracelet/glamour) 渲染为终端 ANSI，自动分页。
- 自有可移植 docset 格式，一条命令导出/导入，方便在联网机器与离线机器之间搬运。

## 构建

需要 Go 1.27+（`go.mod` 中的版本）。普通构建：

```console
$ go build -o termdoc ./cmd/termdoc
```

静态单文件构建（Linux/macOS/Windows 均可交叉编译）：

```console
$ CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o termdoc ./cmd/termdoc
```

例如给没有网络的 Linux 服务器编译：

```console
$ CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o termdoc ./cmd/termdoc
```

> 读取 Dash 的 SQLite 索引使用纯 Go 的 `modernc.org/sqlite`，因此不需要 CGO。

## 快速开始

```console
# 查看有哪些文档可以下载（需要网络）
$ termdoc docs list --remote --source devdocs -q go
$ termdoc docs list --remote --source dash -q python

# 安装（自动在 devdocs 与 Dash 目录中匹配；也可显式 --source）
$ termdoc docs install go
$ termdoc docs install python~3.13
$ termdoc docs install --source dash Go

# 搜索与阅读
$ termdoc search fmt.Println
$ termdoc fmt.Println              # 省略 search 也可以
$ termdoc show fmt.Println
$ termdoc show go:fmt.Println      # 指定 docset，避免同名歧义
$ termdoc show --first context     # 多个同名条目时取第一个

# 管理
$ termdoc docs list
$ termdoc docs info go
$ termdoc docs remove go
$ termdoc docs clean               # 清理下载缓存
```

## 离线工作流

这是 `termdoc` 的主要使用场景。联网机器和工作机器之间只需要传一个归档文件。

**在联网机器上：**

```console
$ termdoc docs install go python~3.13 redis
$ termdoc docs export --all -o termdoc-docs.tar.gz
```

也可以直接从 Zeal 的下载站抓取原始 docset，或使用浏览器下载：

- devdocs：`https://documents.devdocs.io/<slug>/db.json` + `index.json`
- Dash/Zeal：`https://go.zealdocs.org/d/com.kapeli/<Name>/latest`（会重定向到实际归档）

**把 `termdoc-docs.tar.gz` 拷到离线机器后：**

```console
$ termdoc docs import termdoc-docs.tar.gz
$ termdoc docs install ./Go.tgz        # install 也接受本地归档，等价于 import
$ termdoc search Println
```

`docs import` 支持以下输入：

| 输入 | 说明 |
| --- | --- |
| `*.tar.gz`（termdoc 导出） | 可包含多个 docset，根目录有 `termdoc-export.json` 标记 |
| `*.docset/` 目录 | Dash/Zeal 原生 docset |
| `*.tgz` / `*.tar.gz` / `*.zip` | Dash/Zeal 归档 |
| termdoc docset 目录 | 含 `meta.json`、`entries.json`、`search.gob`、`pages.zip` |
| `-` | 从标准输入读取归档 |

`docs export -o -` 可以把归档写到标准输出，配合 `docs import -` 就能通过 SSH 直接搬运：

```console
$ termdoc docs export --all -o - | ssh server 'termdoc docs import -'
```

## 命令

```
termdoc [query]                      搜索（省略 search 的快捷方式）
termdoc search <query>               搜索已安装文档
termdoc show <name>                  阅读条目
termdoc docs list                    列出已安装文档
termdoc docs list --remote           列出可下载文档
termdoc docs install <name>...       下载并安装（也可传本地文件/目录）
termdoc docs import <path>...        从本地归档/目录导入
termdoc docs export [name...]        导出为可移植归档
termdoc docs remove <name>...        删除已安装文档
termdoc docs info [name]             查看文档或数据目录信息
termdoc docs clean                   清理下载缓存
termdoc config                       显示路径与配置
termdoc config --init                生成带注释的 config.toml
termdoc version                      版本信息
```

常用全局参数：

| 参数 | 说明 |
| --- | --- |
| `--home <dir>` | 数据目录，等价于 `$TERMDOC_HOME` |
| `--proxy <url>` | 下载代理，如 `http://127.0.0.1:50000`、`socks5://127.0.0.1:1080` |
| `--style <name>` | 渲染样式：`auto`、`dark`、`light`、`notty` 等 |
| `--width <n>` | 正文换行宽度，0 表示终端宽度 |
| `--no-color` | 禁用颜色 |
| `--no-pager` | 不使用分页器 |
| `--quiet` | 不显示下载进度 |

搜索相关：

```console
$ termdoc search Println -n 10 --json
$ termdoc search Println -d go            # 限定 docset
$ termdoc search Println --all            # 包含 Sample/Example/Guide 条目
```

阅读相关：

```console
$ termdoc show fmt.Println --md           # 输出 Markdown
$ termdoc show fmt.Println --html         # 输出提取后的 HTML 片段
```

## 数据目录

默认位置：

| 平台 | 路径 |
| --- | --- |
| Linux | `$XDG_DATA_HOME/termdoc` 或 `~/.local/share/termdoc` |
| macOS | `~/Library/Application Support/termdoc` |
| Windows | `%LOCALAPPDATA%\termdoc` |

可用 `TERMDOC_HOME` 或 `--home` 覆盖。目录结构：

```
termdoc/
├── config.toml
├── docsets/
│   └── go/
│       ├── meta.json       # 标题、版本、来源、条目数、安装时间
│       ├── entries.json    # 条目：name / type / page / anchor
│       ├── search.gob      # 倒排索引（gob 编码）
│       └── pages.zip       # 原始 HTML 页面
├── cache/                  # 目录缓存与下载缓存
└── tmp/                    # 安装过程中的临时文件
```

每个 docset 都是自包含的，直接拷贝目录也能用；导出格式就是把这些目录打成一个 tar.gz。

## 配置

`config.toml`（全部可选）：

```toml
# 下载代理。环境变量 TERMDOC_PROXY / HTTPS_PROXY / HTTP_PROXY 优先级更高。
proxy = "http://127.0.0.1:50000"

# 分页器。留空则自动选择 less -R / more。
pager = "less -R"

# 渲染样式：auto、dark、light、notty、ascii、dracula、tokyo-night 等。
theme = "auto"

# 换行宽度，0 表示终端宽度。
width = 0

# 禁用分页器。
no_pager = false
```

生成模板：

```console
$ termdoc config --init
```

## 工作原理

### devdocs.io

1. 读取 `https://devdocs.io/docs.json` 得到文档列表。
2. 下载 `https://documents.devdocs.io/<slug>/index.json`（条目索引）和 `db.json`（页面 HTML）。
3. `db.json` 是 `{ "path": "<html>", ... }`，流式解析以避免大文件全部进内存。
4. 条目路径形如 `fmt.html#Println`，按 `id` 找到 heading，只截取该 section。

### Zeal / Dash

1. 读取 `https://api.zealdocs.org/v1/docsets` 得到目录。
2. 通过 `https://go.zealdocs.org/d/<sourceId>/<Name>/latest` 下载（支持官方与用户贡献 feed）。
3. 解包后读取 `Contents/Resources/docSet.dsidx`（SQLite 表 `searchIndex`）。
4. 页面位于 `Contents/Resources/Documents/`，按 `dashAnchor` / `dash_ref_*` 锚点截取条目正文。

### 搜索与渲染

- 建索引时把名称与类型拆成 token：标点分词 + camelCase 子词（如 `getElementById` → `get`、`element`、`by`、`id`）。
- 查询支持多词 AND、精确匹配加权、前缀匹配、类型匹配；无结果时退化为 OR。
- 正文提取 → `html-to-markdown` → glamour ANSI；`less -R` 分页。
- 默认过滤 `Sample` / `Example` / `Guide` 等辅助条目，`--all` 可显示。

## 已知限制

- 搜索只覆盖条目名称与类型，不检索正文（与 Zeal/Dash 一致，速度快、体积小）。
- 不执行页面里的 JavaScript；对依赖 JS 渲染的文档站点效果有限。
- Dash 归档只保留 `.dsidx` 与 HTML 页面，图片、CSS、JS 会被丢弃。
- 暂不支持增量更新；重新安装请加 `--force`。
- TUI 尚未实现。

## 路线图

- [x] CLI：搜索、阅读、安装、导入导出
- [x] devdocs.io 与 Zeal/Dash 两种来源
- [x] 静态单二进制
- [x] 从 stdin 导入归档
- [ ] 交互式 TUI（模糊搜索、结果预览、docset 管理）
- [ ] 本地 `.docset` 生成（导出为 Zeal 兼容格式）
- [ ] 正文全文检索（可选开关）

## 开发

```console
$ go test ./...
$ go vet ./...
$ gofmt -l cmd internal
```

代码结构：

```
cmd/termdoc/          入口
internal/archive/     导出 / 导入
internal/cli/         cobra 命令
internal/config/      数据目录与 config.toml
internal/httpx/       带代理、重试、进度条的 HTTP 客户端
internal/model/       Entry / Meta / Result
internal/render/      HTML 提取、Markdown 转换、ANSI 渲染与分页
internal/source/      devdocs 与 Dash 目录、下载、安装
internal/store/       docset 存储、构建、倒排索引
```
