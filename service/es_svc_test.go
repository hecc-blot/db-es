package service

import (
	"encoding/json"
	"strings"
	"testing"

	dbEsConf "github.com/hecc-blot/db-es/config"

	"github.com/stretchr/testify/assert"
)

func TestBuildEsConfig(t *testing.T) {
	cfg := &dbEsConf.EsConfig{
		Addresses: []string{"http://127.0.0.1:9200"},
		Username:  "elastic",
		Password:  "secret",
	}
	esCfg := buildEsConfig(cfg)

	assert.Equal(t, cfg.Addresses, esCfg.Addresses)
	assert.Equal(t, "elastic", esCfg.Username)
	assert.Equal(t, "secret", esCfg.Password)
	assert.Empty(t, esCfg.APIKey)
	assert.Empty(t, esCfg.CloudID)
	assert.NotNil(t, esCfg.Instrumentation)
}

func TestToJson(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		b, err := toJson(nil)
		assert.NoError(t, err)
		assert.Nil(t, b)
	})

	t.Run("字符串原样", func(t *testing.T) {
		b, err := toJson(`{"match_all":{}}`)
		assert.NoError(t, err)
		assert.Equal(t, `{"match_all":{}}`, string(b))
	})

	t.Run("字节原样", func(t *testing.T) {
		b, err := toJson([]byte(`{"match_all":{}}`))
		assert.NoError(t, err)
		assert.Equal(t, `{"match_all":{}}`, string(b))
	})

	t.Run("结构体序列化", func(t *testing.T) {
		b, err := toJson(map[string]interface{}{"match_all": map[string]interface{}{}})
		assert.NoError(t, err)
		assert.JSONEq(t, `{"match_all":{}}`, string(b))
	})
}

func TestBuildBulkBody(t *testing.T) {
	body, err := buildBulkBody("users", map[string]interface{}{
		"1": map[string]interface{}{"name": "tom"},
	})
	assert.NoError(t, err)

	// 每对 (action, doc) 两行，行数 == 文档数 * 2
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	assert.Len(t, lines, 2)
	assert.JSONEq(t, `{"index":{"_index":"users","_id":"1"}}`, lines[0])
	assert.JSONEq(t, `{"name":"tom"}`, lines[1])
}

func TestDecodeSources(t *testing.T) {
	type doc struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	sources := []json.RawMessage{
		json.RawMessage(`{"name":"tom","age":20}`),
		json.RawMessage(`{"name":"jerry","age":18}`),
	}

	var docs []doc
	assert.NoError(t, decodeSources(sources, &docs))
	assert.Len(t, docs, 2)
	assert.Equal(t, "tom", docs[0].Name)
	assert.Equal(t, 20, docs[0].Age)
	assert.Equal(t, "jerry", docs[1].Name)
}
