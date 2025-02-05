package impl

import (
	"fmt"
	"github.com/nacos-group/nacos-controller/pkg/nacos/auth"
	"github.com/nacos-group/nacos-controller/pkg/nacos/client"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"strconv"
	"strings"
	"sync"
)

type ClientBuilder struct {
	cache sync.Map
}

var builder = ClientBuilder{
	cache: sync.Map{},
}

func GetNacosClientBuilder() *ClientBuilder {
	return &builder
}

func (m *ClientBuilder) Remove(nacosServerParam client.NacosServerParam, key types.NamespacedName) {
	cacheKey := fmt.Sprintf("%s-%s-%s-%s-%s", nacosServerParam.Endpoint, nacosServerParam.ServerAddr, nacosServerParam.Namespace, key.Namespace, key.Name)
	cachedClient, ok := m.cache.Load(cacheKey)
	if ok && cachedClient != nil {
		cachedClient.(config_client.IConfigClient).CloseClient()
	}
	m.cache.Delete(cacheKey)
	return
}

func (m *ClientBuilder) Get(nacosServerParam client.NacosServerParam, key types.NamespacedName) (config_client.IConfigClient, error) {
	cacheKey := fmt.Sprintf("%s-%s-%s-%s-%s", nacosServerParam.Endpoint, nacosServerParam.ServerAddr, nacosServerParam.Namespace, key.Namespace, key.Name)
	cachedClient, ok := m.cache.Load(cacheKey)
	if ok && cachedClient != nil {
		return cachedClient.(config_client.IConfigClient), nil
	}
	return nil, fmt.Errorf("empty DynamicConfiguration")
}

func (m *ClientBuilder) Build(authProvider auth.NacosAuthProvider, authRef *v1.ObjectReference, nacosServerParam client.NacosServerParam, key types.NamespacedName) (config_client.IConfigClient, error) {
	cacheKey := fmt.Sprintf("%s-%s-%s-%s-%s", nacosServerParam.Endpoint, nacosServerParam.ServerAddr, nacosServerParam.Namespace, key.Namespace, key.Name)
	cachedClient, ok := m.cache.Load(cacheKey)
	fmt.Println("try build nacos client, cacheKey:" + cacheKey)
	if ok && cachedClient != nil {
		fmt.Println("cacheKey:" + cacheKey + " exist, return old one")
		return cachedClient.(config_client.IConfigClient), nil
	}
	fmt.Println("cacheKey:" + cacheKey + " not exist, build new one")
	clientParams, err := authProvider.GetNacosClientParams(authRef, nacosServerParam, key)
	if err != nil {
		return nil, err
	}
	var sc []constant.ServerConfig
	clientOpts := []constant.ClientOption{
		constant.WithAccessKey(clientParams.AuthInfo.AccessKey),
		constant.WithSecretKey(clientParams.AuthInfo.SecretKey),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir("/tmp/nacos/log"),
		constant.WithCacheDir("/tmp/nacos/cache"),
		constant.WithLogLevel("debug"),
		constant.WithNamespaceId(clientParams.Namespace),
	}
	if len(clientParams.Endpoint) > 0 {
		clientOpts = append(clientOpts, constant.WithEndpoint(clientParams.Endpoint))
	} else if len(clientParams.ServerAddr) > 0 {
		port := 8848
		ip := clientParams.ServerAddr
		if strings.Contains(ip, ":") {
			split := strings.Split(ip, ":")
			ip = split[0]
			if v, err := strconv.Atoi(split[1]); err != nil {
				return nil, fmt.Errorf("invalid ServerAddr: %s", clientParams.ServerAddr)
			} else {
				port = v
			}

		}
		sc = []constant.ServerConfig{
			*constant.NewServerConfig(ip, uint64(port)),
		}
	}
	cc := *constant.NewClientConfig(clientOpts...)
	configClient, err := clients.NewConfigClient(
		vo.NacosClientParam{
			ClientConfig:  &cc,
			ServerConfigs: sc,
		})
	if err != nil {
		return nil, err
	}
	m.cache.Store(cacheKey, configClient)
	return configClient, nil
}

// DefaultNacosConfigClient 基于Nacos SDK GO 实现配置操作
type DefaultNacosConfigClient struct {
	authProvider  auth.NacosAuthProvider
	clientBuilder *ClientBuilder
}

func (c *DefaultNacosConfigClient) CancelListenConfig(param client.NacosConfigParam) error {
	proxyClient, err := c.clientBuilder.Get(param.NacosServerParam, param.Key)
	if err != nil {
		return fmt.Errorf("get proxyClient failed", err)
	}
	return proxyClient.CancelListenConfig(vo.ConfigParam{
		Group:  param.Group,
		DataId: param.DataId,
	})
}

func (c *DefaultNacosConfigClient) GetConfig(param client.NacosConfigParam) (string, error) {
	proxyClient, err := c.clientBuilder.Build(c.authProvider, param.AuthRef, param.NacosServerParam, param.Key)
	if err != nil {
		return "", err
	}
	return proxyClient.GetConfig(vo.ConfigParam{
		Group:  param.Group,
		DataId: param.DataId,
	})
}

func (c *DefaultNacosConfigClient) PublishConfig(param client.NacosConfigParam) (bool, error) {
	proxyClient, err := c.clientBuilder.Build(c.authProvider, param.AuthRef, param.NacosServerParam, param.Key)
	if err != nil {
		return false, err
	}
	return proxyClient.PublishConfig(vo.ConfigParam{
		Group:   param.Group,
		DataId:  param.DataId,
		Content: param.Content,
	})
}

func (c *DefaultNacosConfigClient) DeleteConfig(param client.NacosConfigParam) (bool, error) {
	proxyClient, err := c.clientBuilder.Build(c.authProvider, param.AuthRef, param.NacosServerParam, param.Key)
	if err != nil {
		return false, err
	}
	return proxyClient.DeleteConfig(vo.ConfigParam{
		Group:  param.Group,
		DataId: param.DataId,
	})
}

func (c *DefaultNacosConfigClient) ListenConfig(param client.NacosConfigParam) error {
	proxyClient, err := c.clientBuilder.Build(c.authProvider, param.AuthRef, param.NacosServerParam, param.Key)
	if err != nil {
		return err
	}
	return proxyClient.ListenConfig(vo.ConfigParam{
		Group:    param.Group,
		DataId:   param.DataId,
		OnChange: param.OnChange,
	})
}

func (c *DefaultNacosConfigClient) CloseClient(param client.NacosConfigParam) {
	c.clientBuilder.Remove(param.NacosServerParam, param.Key)
}

func NewDefaultNacosConfigClient(p auth.NacosAuthProvider) client.NacosConfigClient {
	return &DefaultNacosConfigClient{
		authProvider:  p,
		clientBuilder: GetNacosClientBuilder(),
	}
}
