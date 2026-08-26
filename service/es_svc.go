package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	dbEsConf "github.com/hecc-blot/db-es/config"
	dbEsContract "github.com/hecc-blot/db-es/contract"
	"github.com/hecc-blot/framework/contract/log"
	"github.com/hecc-blot/framework/util"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"go.uber.org/zap"
)

// EsSvc Elasticsearch 搜索数据库服务
type EsSvc struct {
	ctx           context.Context
	client        *elasticsearch.Client
	index         string
	logger        log.ILog
	slowThreshold time.Duration
}

// 编译期断言：确保 EsSvc 实现 IDbSearch
var _ dbEsContract.IDbSearch = (*EsSvc)(nil)

// NewEs 创建单个 Elasticsearch 实例，返回实例与清理函数
func NewEs(config *dbEsConf.EsConfig, logger log.ILog) (dbEsContract.IDbSearch, func(), error) {
	connectTimeout := time.Duration(config.ConnectTimeout) * time.Second
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}

	client, err := elasticsearch.NewClient(buildEsConfig(config))
	if err != nil {
		return nil, func() {}, err
	}

	// fail-fast：启动时 Ping，地址/认证错误立即暴露
	pingCtx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	res, err := client.Ping(client.Ping.WithContext(pingCtx))
	if err != nil {
		return nil, func() {}, err
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, func() {}, fmt.Errorf("es: ping 失败 %s", res.Status())
	}

	return &EsSvc{
		client:        client,
		logger:        logger,
		slowThreshold: time.Duration(config.SlowThreshold) * time.Millisecond,
	}, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Close(ctx)
	}, nil
}

// buildEsConfig 组装 go-elasticsearch 配置，并挂载原生 OpenTelemetry 追踪
func buildEsConfig(config *dbEsConf.EsConfig) elasticsearch.Config {
	return elasticsearch.Config{
		Addresses:       config.Addresses,
		Username:        config.Username,
		Password:        config.Password,
		APIKey:          config.ApiKey,
		CloudID:         config.CloudId,
		Instrumentation: elasticsearch.NewOpenTelemetryInstrumentation(nil, false),
	}
}

// WithContext 设置上下文 — 返回副本，不修改原实例，保证并发安全
func (e *EsSvc) WithContext(ctx context.Context) dbEsContract.IDbSearch {
	ctx = util.ExtractContext(ctx)
	c := *e
	c.ctx = ctx
	return &c
}

// GetInstance 返回底层 elasticsearch.Client 实例，供执行高级操作
func (e *EsSvc) GetInstance() any {
	return e.client
}

// Index 指定索引 — 返回副本，不修改原实例
func (e *EsSvc) Index(name string) dbEsContract.IDbSearch {
	c := *e
	c.index = name
	return &c
}

// IndexDoc 索引/覆盖单条文档
func (e *EsSvc) IndexDoc(id string, doc interface{}) error {
	if err := e.checkIndex(); err != nil {
		return err
	}
	begin := time.Now()
	defer e.logSlow(begin, "index")

	body, err := toJson(doc)
	if err != nil {
		return err
	}
	res, err := e.client.Index(e.index, bytes.NewReader(body),
		e.client.Index.WithContext(e.ctx),
		e.client.Index.WithDocumentID(id),
	)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return e.errorFrom(res)
	}
	return nil
}

// BulkIndex 批量索引文档，key 为文档 ID
func (e *EsSvc) BulkIndex(docs map[string]interface{}) error {
	if err := e.checkIndex(); err != nil {
		return err
	}
	begin := time.Now()
	defer e.logSlow(begin, "bulk")

	body, err := buildBulkBody(e.index, docs)
	if err != nil {
		return err
	}
	res, err := e.client.Bulk(bytes.NewReader(body), e.client.Bulk.WithContext(e.ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return e.errorFrom(res)
	}
	return nil
}

// DeleteDoc 按 ID 删除文档
func (e *EsSvc) DeleteDoc(id string) error {
	if err := e.checkIndex(); err != nil {
		return err
	}
	begin := time.Now()
	defer e.logSlow(begin, "delete")

	res, err := e.client.Delete(e.index, id, e.client.Delete.WithContext(e.ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return e.errorFrom(res)
	}
	return nil
}

// Get 按 ID 获取文档，_source 解码到 dst
func (e *EsSvc) Get(id string, dst interface{}) error {
	if err := e.checkIndex(); err != nil {
		return err
	}
	begin := time.Now()
	defer e.logSlow(begin, "get")

	res, err := e.client.Get(e.index, id, e.client.Get.WithContext(e.ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return e.errorFrom(res)
	}

	var resp struct {
		Found  bool            `json:"found"`
		Source json.RawMessage `json:"_source"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return err
	}
	if !resp.Found {
		return fmt.Errorf("es: 文档不存在 %s/%s", e.index, id)
	}
	return json.Unmarshal(resp.Source, dst)
}

// Search 按 Query DSL 搜索，命中文档 _source 解码到 dst（*[]T）
func (e *EsSvc) Search(query interface{}, dst interface{}) error {
	if err := e.checkIndex(); err != nil {
		return err
	}
	begin := time.Now()
	defer e.logSlow(begin, "search")

	qb, err := toJson(query)
	if err != nil {
		return err
	}
	opts := []func(*esapi.SearchRequest){
		e.client.Search.WithContext(e.ctx),
		e.client.Search.WithIndex(e.index),
	}
	if qb != nil {
		opts = append(opts, e.client.Search.WithBody(bytes.NewReader(qb)))
	}

	res, err := e.client.Search(opts...)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return e.errorFrom(res)
	}

	var resp struct {
		Hits struct {
			Hits []struct {
				Source json.RawMessage `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return err
	}

	sources := make([]json.RawMessage, 0, len(resp.Hits.Hits))
	for _, h := range resp.Hits.Hits {
		sources = append(sources, h.Source)
	}
	return decodeSources(sources, dst)
}

// Count 按 Query DSL 统计文档数
func (e *EsSvc) Count(query interface{}) (int64, error) {
	if err := e.checkIndex(); err != nil {
		return 0, err
	}
	begin := time.Now()
	defer e.logSlow(begin, "count")

	qb, err := toJson(query)
	if err != nil {
		return 0, err
	}
	opts := []func(*esapi.CountRequest){
		e.client.Count.WithContext(e.ctx),
		e.client.Count.WithIndex(e.index),
	}
	if qb != nil {
		opts = append(opts, e.client.Count.WithBody(bytes.NewReader(qb)))
	}

	res, err := e.client.Count(opts...)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.IsError() {
		return 0, e.errorFrom(res)
	}

	var resp struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return 0, err
	}
	return resp.Count, nil
}

// ---- 以下为内部辅助函数 ----

// toJson 将 query/doc 转为 JSON 字节；nil 返回 nil（表示无 body）
func toJson(v interface{}) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(t), nil
	case []byte:
		return t, nil
	default:
		return json.Marshal(v)
	}
}

// decodeSources 将命中文档的 _source 解码到 dst（*[]T）
func decodeSources(sources []json.RawMessage, dst interface{}) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Slice {
		return errors.New("es: dst 必须是 *[]T 类型")
	}
	slice := v.Elem()
	elemType := slice.Type().Elem()
	for _, src := range sources {
		elem := reflect.New(elemType)
		if err := json.Unmarshal(src, elem.Interface()); err != nil {
			return err
		}
		slice = reflect.Append(slice, elem.Elem())
	}
	v.Elem().Set(slice)
	return nil
}

// buildBulkBody 组装 bulk NDJSON 请求体
func buildBulkBody(index string, docs map[string]interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for id, doc := range docs {
		if err := enc.Encode(map[string]interface{}{
			"index": map[string]interface{}{
				"_index": index,
				"_id":    id,
			},
		}); err != nil {
			return nil, err
		}
		if err := enc.Encode(doc); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func (e *EsSvc) checkIndex() error {
	if e.index == "" {
		return errors.New("es: 未指定索引，请先调用 Index(name)")
	}
	return nil
}

func (e *EsSvc) errorFrom(res *esapi.Response) error {
	b, _ := io.ReadAll(res.Body)
	return fmt.Errorf("es: 请求失败 %s %s", res.Status(), string(b))
}

func (e *EsSvc) logSlow(begin time.Time, op string) {
	if e.slowThreshold <= 0 {
		return
	}
	if elapsed := time.Since(begin); elapsed > e.slowThreshold {
		e.logger.Warn(e.ctx, "Slow ES", zap.Duration("elapsed", elapsed), zap.String("op", op))
	}
}
