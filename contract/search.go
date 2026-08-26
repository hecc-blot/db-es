package contract

import (
	"context"

	dbContract "github.com/hecc-blot/db/contract"
)

// IDbSearch 搜索型数据库接口（Elasticsearch）。
// 复用 db 模块的 IDbBase（GetInstance），自身提供索引 + 文档写入 + 搜索查询能力。
// query 为 ES 的 Query DSL（map[string]interface{} 或结构体），doc/dst 为业务文档。
type IDbSearch interface {
	dbContract.IDbBase
	WithContext(ctx context.Context) IDbSearch

	// Index 指定索引 — 返回副本，不修改原实例
	Index(name string) IDbSearch

	// 写入
	IndexDoc(id string, doc interface{}) error
	BulkIndex(docs map[string]interface{}) error
	DeleteDoc(id string) error

	// 读取
	Get(id string, dst interface{}) error
	Search(query interface{}, dst interface{}) error // dst 为 *[]T，提取命中文档 _source
	Count(query interface{}) (int64, error)
}
