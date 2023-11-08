package config

import (
	"errors"
	nacosv1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/pkg/nacos/auth"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type NacosConfigClient interface {
	GetConfig(param NacosConfigParam) (string, error)
	PublishConfig(param NacosConfigParam) (bool, error)
	DeleteConfig(param NacosConfigParam) (bool, error)
	ListenConfig(param NacosConfigParam) error
	CancelListenConfig(param NacosConfigParam) error
}

type DefaultNacosConfigClient struct {
	authProvider auth.NacosAuthProvider
	authManager  *auth.NacosAuthManager
}

func (c *DefaultNacosConfigClient) CancelListenConfig(param NacosConfigParam) error {
	if param.DynamicConfiguration == nil {
		return errors.New("empty DynamicConfiguration")
	}
	proxyClient, err := c.authManager.GetNacosConfigClient(c.authProvider, param.DynamicConfiguration)
	if err != nil {
		return err
	}
	return proxyClient.CancelListenConfig(vo.ConfigParam{
		Group:  param.Group,
		DataId: param.DataId,
	})
}

func (c *DefaultNacosConfigClient) GetConfig(param NacosConfigParam) (string, error) {
	if param.DynamicConfiguration == nil {
		return "", errors.New("empty DynamicConfiguration")
	}
	proxyClient, err := c.authManager.GetNacosConfigClient(c.authProvider, param.DynamicConfiguration)
	if err != nil {
		return "", err
	}
	return proxyClient.GetConfig(vo.ConfigParam{
		Group:  param.Group,
		DataId: param.DataId,
	})
}

func (c *DefaultNacosConfigClient) PublishConfig(param NacosConfigParam) (bool, error) {
	if param.DynamicConfiguration == nil {
		return false, errors.New("empty DynamicConfiguration")
	}
	proxyClient, err := c.authManager.GetNacosConfigClient(c.authProvider, param.DynamicConfiguration)
	if err != nil {
		return false, err
	}
	return proxyClient.PublishConfig(vo.ConfigParam{
		Group:   param.Group,
		DataId:  param.DataId,
		Content: param.Content,
	})
}

func (c *DefaultNacosConfigClient) DeleteConfig(param NacosConfigParam) (bool, error) {
	if param.DynamicConfiguration == nil {
		return false, errors.New("empty DynamicConfiguration")
	}
	proxyClient, err := c.authManager.GetNacosConfigClient(c.authProvider, param.DynamicConfiguration)
	if err != nil {
		return false, err
	}
	return proxyClient.DeleteConfig(vo.ConfigParam{
		Group:  param.Group,
		DataId: param.DataId,
	})
}

func (c *DefaultNacosConfigClient) ListenConfig(param NacosConfigParam) error {
	if param.DynamicConfiguration == nil {
		return errors.New("empty DynamicConfiguration")
	}
	proxyClient, err := c.authManager.GetNacosConfigClient(c.authProvider, param.DynamicConfiguration)
	if err != nil {
		return err
	}
	return proxyClient.ListenConfig(vo.ConfigParam{
		Group:    param.Group,
		DataId:   param.DataId,
		OnChange: param.OnChange,
	})
}

func NewDefaultNacosConfigClient(p auth.NacosAuthProvider, m *auth.NacosAuthManager) NacosConfigClient {
	return &DefaultNacosConfigClient{
		authProvider: p,
		authManager:  m,
	}
}

type NacosConfigParam struct {
	DynamicConfiguration *nacosv1.DynamicConfiguration
	DataId               string
	Group                string
	Content              string
	OnChange             func(namespace, group, dataId, data string)
}
