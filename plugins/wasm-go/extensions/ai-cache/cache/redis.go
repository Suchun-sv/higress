package cache

import (
	"errors"

	"github.com/alibaba/higress/plugins/wasm-go/pkg/wrapper"
)

type redisProviderInitializer struct {
}

func (r *redisProviderInitializer) ValidateConfig(cf ProviderConfig) error {
	if len(cf.serviceName) == 0 {
		return errors.New("cache service name is required")
	}
	return nil
}

func (r *redisProviderInitializer) CreateProvider(cf ProviderConfig) (Provider, error) {
	rp := redisProvider{
		config: cf,
		client: wrapper.NewRedisClusterClient(wrapper.FQDNCluster{
			FQDN: cf.serviceName,
			Host: cf.serviceHost,
			Port: int64(cf.servicePort)}),
	}
	err := rp.Init(cf.username, cf.password, cf.timeout)
	return &rp, err
}

type redisProvider struct {
	config ProviderConfig
	client wrapper.RedisClient
}

func (rp *redisProvider) GetProviderType() string {
	return PROVIDER_TYPE_REDIS
}

func (rp *redisProvider) Init(username string, password string, timeout uint32) error {
	return rp.client.Init(rp.config.username, rp.config.password, int64(rp.config.timeout))
}

func (rp *redisProvider) Get(key string, cb wrapper.RedisResponseCallback) error {
	return rp.client.Get(rp.getCacheKeyPrefix()+key, cb)
}

func (rp *redisProvider) Set(key string, value string, cb wrapper.RedisResponseCallback) error {
	return rp.client.SetEx(rp.getCacheKeyPrefix()+key, value, int(rp.config.cacheTTL), cb)
}

func (rp *redisProvider) getCacheKeyPrefix() string {
	return rp.config.cacheKeyPrefix
}
