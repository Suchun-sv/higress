package cache

import (
	"errors"

	"github.com/alibaba/higress/plugins/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
)

const (
	PROVIDER_TYPE_REDIS  = "redis"
	DEFAULT_CACHE_PREFIX = "higressAiCache:"
)

type providerInitializer interface {
	ValidateConfig(ProviderConfig) error
	CreateProvider(ProviderConfig) (Provider, error)
}

var (
	providerInitializers = map[string]providerInitializer{
		PROVIDER_TYPE_REDIS: &redisProviderInitializer{},
	}
)

type ProviderConfig struct {
	// @Title zh-CN redis 缓存服务提供者类型
	// @Description zh-CN 缓存服务提供者类型，例如 redis
	typ string
	// @Title zh-CN redis 缓存服务名称
	// @Description zh-CN 缓存服务名称
	serviceName string
	// @Title zh-CN redis 缓存服务端口
	// @Description zh-CN 缓存服务端口，默认值为6379
	servicePort int
	// @Title zh-CN redis 缓存服务地址
	// @Description zh-CN Cache 缓存服务地址，非必填
	serviceHost string
	// @Title zh-CN 缓存服务用户名
	// @Description zh-CN 缓存服务用户名，非必填
	userName string
	// @Title zh-CN 缓存服务密码
	// @Description zh-CN 缓存服务密码，非必填
	password string
	// @Title zh-CN 请求超时
	// @Description zh-CN 请求缓存服务的超时时间，单位为毫秒。默认值是10000，即10秒
	timeout uint32
	// @Title zh-CN 缓存过期时间
	// @Description zh-CN 缓存过期时间，单位为秒。默认值是3600000，即1小时
	cacheTTL uint32
	// @Title 缓存 Key 前缀
	// @Description 缓存 Key 的前缀，默认值为 "higressAiCache:"
	cacheKeyPrefix string
}

func (c *ProviderConfig) FromJson(json gjson.Result) {
	c.typ = json.Get("type").String()
	c.serviceName = json.Get("serviceName").String()
	c.servicePort = int(json.Get("servicePort").Int())
	if !json.Get("servicePort").Exists() {
		c.servicePort = 6379
	}
	c.serviceHost = json.Get("serviceHost").String()
	c.userName = json.Get("username").String()
	if !json.Get("username").Exists() {
		c.userName = ""
	}
	c.password = json.Get("password").String()
	if !json.Get("password").Exists() {
		c.password = ""
	}
	c.timeout = uint32(json.Get("timeout").Int())
	if !json.Get("timeout").Exists() {
		c.timeout = 10000
	}
	c.cacheTTL = uint32(json.Get("cacheTTL").Int())
	if !json.Get("cacheTTL").Exists() {
		c.cacheTTL = 3600000
	}
	c.cacheKeyPrefix = json.Get("cacheKeyPrefix").String()
	if !json.Get("cacheKeyPrefix").Exists() {
		c.cacheKeyPrefix = DEFAULT_CACHE_PREFIX
	}
}

func (c *ProviderConfig) Validate() error {
	if c.typ == "" {
		return errors.New("cache service type is required")
	}
	if c.serviceName == "" {
		return errors.New("cache service name is required")
	}
	initializer, has := providerInitializers[c.typ]
	if !has {
		return errors.New("unknown cache service provider type: " + c.typ)
	}
	if err := initializer.ValidateConfig(*c); err != nil {
		return err
	}
	return nil
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
	Init(username string, password string, timeout uint32) error
	Get(key string, cb wrapper.RedisResponseCallback) error
	Set(key string, value string, cb wrapper.RedisResponseCallback) error
	GetCacheKeyPrefix() string
}