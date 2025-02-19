package client

import (
	"github.com/nacos-group/nacos-controller/internal"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/common/logger"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	v1 "k8s.io/api/core/v1"
	"os"
	"path"
)

type NacosOptions struct {
	Namespace string

	// ServersIP are explicitly specified to be connected to nacos by client.
	ServersIP []string

	// ServerPort are explicitly specified to be used when the client connects to nacos.
	ServerPort uint64

	AccessKey string
	SecretKey string
}

func ConvertToNacosClientParam(options NacosOptions) vo.NacosClientParam {
	clientConfig := constant.ClientConfig{
		NamespaceId:         options.Namespace,
		NotLoadCacheAtStart: true,
		LogDir:              path.Join(os.Getenv("HOME"), "logs", "nacos-go-sdk"),
	}

	var serversConfig []constant.ServerConfig
	for _, ip := range options.ServersIP {
		serversConfig = append(serversConfig, constant.ServerConfig{
			IpAddr: ip,
			Port:   options.ServerPort,
		})
	}

	return vo.NacosClientParam{
		ClientConfig:  &clientConfig,
		ServerConfigs: serversConfig,
	}
}

type ServiceKey struct {
	ServiceName string

	Group string
}

type ServiceInfo struct {
	ServiceKey

	Port uint64

	Metadata map[string]string
}

type NamingClient interface {
	RegisterService(ServiceInfo, []Address)

	UnregisterService(ServiceInfo)

	RegisterServiceInstances(serviceInfo ServiceInfo, addresses []Address)

	UnregisterServiceInstances(serviceInfo ServiceInfo, addresses []Address)

	UpdateServiceHealthCheckType(key ServiceKey) bool
}

type namingClient struct {
	client      naming_client.INamingClient
	servicesMap map[ServiceKey][]Address
	httpSdk     *NacosHttpSdk
}

func NewNamingClient(options NacosOptions) (NamingClient, error) {
	nacosConfig := ConvertToNacosClientParam(options)
	client, err := clients.NewNamingClient(nacosConfig)
	if err != nil {
		return nil, err
	}

	return &namingClient{
		client:      client,
		servicesMap: make(map[ServiceKey][]Address),
		httpSdk:     NewNacosHttpSdk(options),
	}, nil
}
func (c *namingClient) UpdateServiceHealthCheckType(key ServiceKey) bool {
	return c.httpSdk.UpdateServiceHealthCheckTypeToNone(key)
}
func (c *namingClient) RegisterService(serviceInfo ServiceInfo, addresses []Address) {
	if c.UpdateServiceHealthCheckType(serviceInfo.ServiceKey) {
		old := c.servicesMap[serviceInfo.ServiceKey]
		added, deleted := diffAddresses(old, addresses)
		logger.Infof("Register service (%s@@%s), added %d, deleted %d.",
			serviceInfo.ServiceName, serviceInfo.Group, len(added), len(deleted))

		c.RegisterServiceInstances(serviceInfo, added)
		c.UnregisterServiceInstances(serviceInfo, deleted)

		c.servicesMap[serviceInfo.ServiceKey] = addresses
	} else {
		logger.Warnf("Register service fail, service (%s@@%s) is not registered.", serviceInfo.ServiceName, serviceInfo.Group)
	}
}

func (c *namingClient) UnregisterService(serviceInfo ServiceInfo) {
	logger.Infof("Unregister service (%s@@%s).", serviceInfo.ServiceName, serviceInfo.Group)
	c.UnregisterServiceInstances(serviceInfo, c.servicesMap[serviceInfo.ServiceKey])
	delete(c.servicesMap, serviceInfo.ServiceKey)
}

func (c *namingClient) RegisterServiceInstances(serviceInfo ServiceInfo, addresses []Address) {
	if !c.UpdateServiceHealthCheckType(serviceInfo.ServiceKey) {
		logger.Warnf("Update service health check type fail, service (%s@@%s) is not registered.", serviceInfo.ServiceName, serviceInfo.Group)
		return
	}
	for _, address := range addresses {
		if _, err := c.client.RegisterInstance(vo.RegisterInstanceParam{
			Ip:          address.IP,
			Port:        address.Port,
			Weight:      internal.DefaultNacosEndpointWeight,
			Enable:      true,
			Healthy:     true,
			Metadata:    serviceInfo.Metadata,
			ServiceName: serviceInfo.ServiceName,
			GroupName:   serviceInfo.Group,
			Ephemeral:   false,
		}); err != nil {
			logger.Errorf("Register instance (%s:%d) with service (%s@@%s) fail, err %v.",
				address.IP, address.Port, serviceInfo.ServiceName, serviceInfo.Group, err)
		}
	}
}

func (c *namingClient) UnregisterServiceInstances(serviceInfo ServiceInfo, addresses []Address) {
	for _, address := range addresses {
		if _, err := c.client.DeregisterInstance(vo.DeregisterInstanceParam{
			Ip:          address.IP,
			Port:        address.Port,
			ServiceName: serviceInfo.ServiceName,
			GroupName:   serviceInfo.Group,
			Ephemeral:   false,
		}); err != nil {
			logger.Errorf("Unregister instance (%s:%d) with service (%s@@%s) fail, err %v.",
				address.IP, address.Port, serviceInfo.ServiceName, serviceInfo.Group, err)
		}
	}
}

type Address struct {
	IP   string `json:"ip"`
	Port uint64 `json:"port"`
}

func diffAddresses(old, curr []Address) ([]Address, []Address) {
	var added, deleted []Address
	oldAddressesSet := make(map[Address]struct{}, len(old))
	newAddressesSet := make(map[Address]struct{}, len(curr))

	for _, s := range old {
		oldAddressesSet[s] = struct{}{}
	}
	for _, s := range curr {
		newAddressesSet[s] = struct{}{}
	}

	for oldAddress := range oldAddressesSet {
		if _, exist := newAddressesSet[oldAddress]; !exist {
			deleted = append(deleted, oldAddress)
		}
	}

	for newAddress := range newAddressesSet {
		if _, exist := oldAddressesSet[newAddress]; !exist {
			added = append(added, newAddress)
		}
	}

	return added, deleted
}

func ConvertToAddresses(realPort uint64, endpoints *v1.Endpoints) []Address {
	var addresses []Address
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			for _, port := range subset.Ports {
				if port.Port == int32(realPort) {
					addresses = append(addresses, Address{
						IP:   address.IP,
						Port: realPort,
					})
				}
			}
		}
	}

	return addresses
}
