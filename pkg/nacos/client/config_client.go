package client

import (
	nacosv1 "github.com/nacos-group/nacos-controller/api/v1"
	"sync"
)

var _defaultClient NacosConfigClient
var _lock = sync.Mutex{}

func RegisterNacosClientIfAbsent(c NacosConfigClient) {
	_lock.Lock()
	defer _lock.Unlock()
	if _defaultClient == nil {
		_defaultClient = c
	}
}

func RegisterNacosClient(c NacosConfigClient) {
	_lock.Lock()
	defer _lock.Unlock()
	_defaultClient = c
}

func GetDefaultNacosClient() NacosConfigClient {
	if _defaultClient == nil {
		panic("No default NacosConfigClient registered")
	}
	return _defaultClient
}

type NacosConfigClient interface {
	GetConfig(param NacosConfigParam) (string, error)
	PublishConfig(param NacosConfigParam) (bool, error)
	DeleteConfig(param NacosConfigParam) (bool, error)
	ListenConfig(param NacosConfigParam) error
	CancelListenConfig(param NacosConfigParam) error
}

type NacosConfigParam struct {
	DynamicConfiguration *nacosv1.DynamicConfiguration
	DataId               string
	Group                string
	Content              string
	OnChange             func(namespace, group, dataId, data string)
}
