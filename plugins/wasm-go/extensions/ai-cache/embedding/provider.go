package embedding

import (
	"errors"
	"net/http"

	"github.com/alibaba/higress/plugins/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
)

const (
	providerTypeDashScope = "dashscope"
)

type providerInitializer interface {
	ValidateConfig(ProviderConfig) error
	CreateProvider(ProviderConfig) (Provider, error)
}

var (
	providerInitializers = map[string]providerInitializer{
		providerTypeDashScope: &dashScopeProviderInitializer{},
	}
)

type ProviderConfig struct {
	// @Title zh-CN 文本特征提取服务提供者类型
	// @Description zh-CN 文本特征提取服务提供者类型，例如 DashScope
	typ string `json:"TextEmbeddingProviderType"`
	// @Title zh-CN DashScope 阿里云大模型服务名
	// @Description zh-CN 调用阿里云的大模型服务
	ServiceName       string             `require:"true" yaml:"DashScopeServiceName" jaon:"DashScopeServiceName"`
	Client            wrapper.HttpClient `yaml:"-"`
	DashScopeKey      string             `require:"true" yaml:"DashScopeKey" jaon:"DashScopeKey"`
	DashScopeTimeout  uint32             `require:"true" yaml:"DashScopeTimeout" jaon:"DashScopeTimeout"`
	QueryEmbeddingKey string             `require:"true" yaml:"QueryEmbeddingKey" jaon:"QueryEmbeddingKey"`
}

func (c *ProviderConfig) FromJson(json gjson.Result) {
	c.typ = json.Get("TextEmbeddingProviderType").String()
	c.ServiceName = json.Get("DashScopeServiceName").String()
	c.DashScopeKey = json.Get("DashScopeKey").String()
	c.DashScopeTimeout = uint32(json.Get("DashScopeTimeout").Int())
	c.QueryEmbeddingKey = json.Get("QueryEmbeddingKey").String()
}

func (c *ProviderConfig) Validate() error {
	if len(c.DashScopeKey) == 0 {
		return errors.New("DashScopeKey is required")
	}
	if len(c.ServiceName) == 0 {
		return errors.New("DashScopeServiceName is required")
	}
	return nil
}

func (c *ProviderConfig) GetProviderType() string {
	return c.typ
}

func CreateProvider(pc ProviderConfig) (Provider, error) {
	initializer, has := providerInitializers[pc.typ]
	if !has {
		return nil, errors.New("unknown provider type: " + pc.typ)
	}
	return initializer.CreateProvider(pc)
}

type Provider interface {
	GetProviderType() string
	GetEmbedding(
		text string,
		ctx wrapper.HttpContext,
		log wrapper.Log,
		callback func(emb []float64, statusCode int, responseHeaders http.Header, responseBody []byte)) error
}
