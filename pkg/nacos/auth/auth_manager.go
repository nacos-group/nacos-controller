package auth

import (
	"fmt"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"strconv"
	"strings"
	"sync"
)

type NacosAuthManager struct {
	cache sync.Map
}

type ConfigClientParam struct {
	Endpoint   string
	ServerAddr string
	Namespace  string
	AuthInfo   ConfigClientAuthInfo
}

type ConfigClientAuthInfo struct {
	AccessKey string
	SecretKey string
}

var manager = NacosAuthManager{
	cache: sync.Map{},
}

func GetNacosAuthManger() *NacosAuthManager {
	return &manager
}

func (m *NacosAuthManager) GetNacosConfigClient(authProvider NacosAuthProvider, dc *nacosiov1.DynamicConfiguration) (config_client.IConfigClient, error) {
	if dc == nil {
		return nil, fmt.Errorf("empty DynamicConfiguration")
	}
	nacosServer := dc.Spec.NacosServer
	// 简化判空逻辑，cacheKey仅内部使用
	cacheKey := fmt.Sprintf("%s-%s-%s", nacosServer.Endpoint, nacosServer.ServerAddr, nacosServer.Namespace)
	cachedClient, ok := m.cache.Load(cacheKey)
	if ok && cachedClient != nil {
		return cachedClient.(config_client.IConfigClient), nil
	}
	clientParams, err := authProvider.GetNacosClientParams(dc)
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
