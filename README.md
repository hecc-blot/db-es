# hecc-blot-db-es

基于 go-elasticsearch/v8 的 Elasticsearch 搜索型数据库组件：索引 + 文档写入、Query DSL 搜索，请求自动接入链路追踪。

## 安装

```bash
go get github.com/hecc-blot/db-es
```

## 接口定义

```go
import (
    dbContract "github.com/hecc-blot/db/contract"
    dbEsContract "github.com/hecc-blot/db-es/contract"
)

type IDbSearch interface {
    dbContract.IDbBase
    WithContext(ctx context.Context) IDbSearch

    Index(name string) IDbSearch

    IndexDoc(id string, doc interface{}) error
    BulkIndex(docs map[string]interface{}) error
    DeleteDoc(id string) error

    Get(id string, dst interface{}) error
    Search(query interface{}, dst interface{}) error
    Count(query interface{}) (int64, error)
}
```

query 为 ES 的 Query DSL（`map[string]interface{}` 或结构体）；`Search` 的 `dst` 为 `*[]T`，自动提取命中文档的 `_source`。

## 初始化

```go
import (
    dbEs "github.com/hecc-blot/db-es/service"
)

esDb, clearUp, err := dbEs.NewEs(&config.Es, logSvc)
if err != nil {
    panic(err)
}
defer clearUp()

container.Set(new(dbEsContract.IDbSearch), esDb)
```

业务方直接注入 `IDbSearch`，每个请求用 `WithContext(ctx)` 取副本：

```go
type ArticleApi struct {
    Search dbEsContract.IDbSearch `inject:""`
}

func (a ArticleApi) Call(ctx *gin.Context) (interface{}, iCoreError.IError) {
    db := a.Search.WithContext(ctx)   // 返回绑定请求上下文的副本，并发安全

    query := map[string]interface{}{
        "match": map[string]interface{}{"title": "golang"},
    }
    var articles []ArticleModel
    if err := db.Index("articles").Search(query, &articles); err != nil {
        return nil, errorSvc.NewError(response.Fail, err)
    }
    return articles, nil
}
```

## CRUD 操作

```go
// 索引 / 批量索引 / 删除
err := db.Index("articles").IndexDoc("1", &ArticleModel{Title: "golang"})
err = db.Index("articles").BulkIndex(map[string]interface{}{
    "2": &ArticleModel{Title: "rust"},
    "3": &ArticleModel{Title: "java"},
})
err = db.Index("articles").DeleteDoc("1")

// 按 ID 获取
var article ArticleModel
err = db.Index("articles").Get("1", &article)

// 搜索 / 统计
query := map[string]interface{}{"match_all": map[string]interface{}{}}
err = db.Index("articles").Search(query, &articles)
count, err := db.Index("articles").Count(query)
```

## 配置

```yaml
es:
  addresses:              # 节点地址列表
    - http://127.0.0.1:9200
  username: elastic
  password: ""
  api_key: ""             # Base64 编码 API Key，设置后覆盖用户名密码
  cloud_id: ""            # Elastic Cloud 连接串，与 addresses 二选一
  connect_timeout: 10     # 建连/Ping 超时（秒），0 用默认 10s
  slow_threshold: 200     # 慢查询阈值（毫秒），0 不记录
```

## 链路追踪

go-elasticsearch v8.19+ 内置 OpenTelemetry 集成，通过 `Config.Instrumentation` 挂载，请求自动生成 span（含 db.elasticsearch 相关语义属性），只依赖第三方 otel，不依赖 trace 模块。初始化顺序要求：先初始化 trace，再初始化 db-es。

## 相关模块

| 模块 | 说明 |
|------|------|
| [db](https://github.com/hecc-blot/db) | 关系型（MySQL / PostgreSQL），提供 `IDbBase` |
| [db-clickhouse](https://github.com/hecc-blot/db-clickhouse) | 分析型（ClickHouse） |
| [db-mongo](https://github.com/hecc-blot/db-mongo) | 文档型（MongoDB） |
