package config

// EsConfig Elasticsearch 连接配置。
// Addresses 为节点地址列表（如 ["http://127.0.0.1:9200"]）。
type EsConfig struct {
	Addresses []string `mapstructure:"addresses"`
	Username  string   `mapstructure:"username"`
	Password  string   `mapstructure:"password"`
	ApiKey    string   `mapstructure:"api_key"`  // Base64 编码 API Key，设置后覆盖用户名密码
	CloudId   string   `mapstructure:"cloud_id"` // Elastic Cloud 连接串，与 Addresses 二选一

	ConnectTimeout int `mapstructure:"connect_timeout"` // 建连/Ping 超时（秒），0 用默认 10s
	SlowThreshold  int `mapstructure:"slow_threshold"`  // 慢查询阈值（毫秒），0 不记录
}
