package naming

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
	"strings"
	"sync"

	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/pkg"
	"github.com/nacos-group/nacos-controller/pkg/nacos/auth"
	"github.com/nacos-group/nacos-controller/pkg/nacos/client"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/common/logger"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type NacosNamingClientBuilder struct {
	cache sync.Map
}

var builder = NacosNamingClientBuilder{
	cache: sync.Map{},
}

func GetNacosNamingClientBuilder() *NacosNamingClientBuilder {
	return &builder
}
func (m *NacosNamingClientBuilder) BuildNamingClient(authProvider auth.NacosAuthProvider, sd *nacosiov1.ServiceDiscovery) (*NacosNamingClient, error) {
	if sd == nil {
		return nil, fmt.Errorf("empty ServiceDiscovery")
	}

	bytes, _ := json.Marshal(sd)
	fmt.Println("BuildNamingClient, sd:", string(bytes))
	bytes, _ = json.Marshal(authProvider)
	fmt.Println("BuildNamingClient, auth:", string(bytes))

	nacosServer := sd.Spec.NacosServer
	// 简化判空逻辑，cacheKey仅内部使用
	cacheKey := fmt.Sprintf("%s-%s", nacosServer.ServerAddr, nacosServer.Namespace)
	cachedClient, ok := m.cache.Load(cacheKey)
	if ok && cachedClient != nil {
		return cachedClient.(*NacosNamingClient), nil
	}
	//clientParams, err := authProvider.GetNacosNamingClientParams(sd)
	nacosServerParam := client.NacosServerParam{
		Endpoint:   nacosServer.Endpoint,
		Namespace:  nacosServer.Namespace,
		ServerAddr: nacosServer.ServerAddr,
	}
	clientParams, err := authProvider.GetNacosClientParams(sd.Spec.NacosServer.AuthRef, nacosServerParam, sd.Namespace)
	if err != nil {
		return nil, err
	}
	var sc []constant.ServerConfig
	clientOpts := []constant.ClientOption{
		constant.WithAccessKey(clientParams.AuthInfo.AccessKey),
		constant.WithSecretKey(clientParams.AuthInfo.SecretKey),
		constant.WithPassword(clientParams.AuthInfo.Password),
		constant.WithPassword(clientParams.AuthInfo.Username),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir("/tmp/nacos/log"),
		constant.WithCacheDir("/tmp/nacos/cache"),
		constant.WithLogLevel("debug"),
		constant.WithNamespaceId(clientParams.Namespace),
		constant.WithAppName("nacos-controller"),
		constant.WithAppConnLabels(map[string]string{"k8s.namespace": sd.Namespace,
			"k8s.cluster": pkg.CurrentContext,
			"k8s.name":    sd.Name}),
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
	nClient, err := clients.NewNamingClient(
		vo.NacosClientParam{
			ClientConfig:  &cc,
			ServerConfigs: sc,
		})
	if err != nil {
		return nil, err
	}

	serverAddrs := []string{clientParams.ServerAddr}
	nClient1 := &NacosNamingClient{
		client:  nClient,
		httpSdk: NewNacosHttpSdk(serverAddrs, clientParams.Namespace, clientParams.AuthInfo.AccessKey, clientParams.AuthInfo.SecretKey),
	}

	m.cache.Store(cacheKey, nClient1)
	return nClient1, nil
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

func (s *ServiceKey) String() string {
	return fmt.Sprintf("%s@@%s", s.ServiceName, s.Group)
}

type ServiceInfo struct {
	ServiceKey

	Port uint64

	Metadata map[string]string
}

type Addresses struct {
	IPAddresses []Address
}
type NamingService interface {
	RegisterService(ServiceInfo, []Address)

	UnregisterService(ServiceInfo)

	RegisterServiceInstances(serviceInfo ServiceInfo, addresses []Address)

	UnregisterServiceInstances(serviceInfo ServiceInfo, addresses []Address)

	UpdateServiceHealthCheckType(key ServiceKey) bool
}

type NacosNamingClient struct {
	client      naming_client.INamingClient
	servicesMap sync.Map
	httpSdk     *NacosHttpSdk
}

func NewNamingClient(options NacosOptions) (*NacosNamingClient, error) {
	nacosConfig := ConvertToNacosClientParam(options)
	client, err := clients.NewNamingClient(nacosConfig)
	if err != nil {
		return nil, err
	}

	return &NacosNamingClient{
		client:  client,
		httpSdk: NewNacosHttpSdk(options.ServersIP, options.Namespace, options.AccessKey, options.SecretKey),
	}, nil
}
func (c *NacosNamingClient) UpdateServiceHealthCheckType(key ServiceKey) bool {
	return c.httpSdk.UpdateServiceHealthCheckTypeToNone(key)
}
func (c *NacosNamingClient) RegisterService(serviceInfo ServiceInfo, addresses []Address) {
	if c.UpdateServiceHealthCheckType(serviceInfo.ServiceKey) {
		old, exist := c.servicesMap.Load(serviceInfo.ServiceKey)
		if !exist {
			logger.Infof("Register service (%s@@%s), added %d, deleted %d.",
				serviceInfo.ServiceName, serviceInfo.Group, len(addresses), 0)
		}

		added, deleted := diffAddresses(old.([]Address), addresses)
		logger.Infof("Register service (%s@@%s), added %d, deleted %d.",
			serviceInfo.ServiceName, serviceInfo.Group, len(added), len(deleted))

		c.RegisterServiceInstances(serviceInfo, added)
		c.UnregisterServiceInstances(serviceInfo, deleted)

		c.servicesMap.Store(serviceInfo.ServiceKey, addresses)
	} else {
		logger.Warnf("Register service fail, service (%s@@%s) is not registered.", serviceInfo.ServiceName, serviceInfo.Group)
	}
}

func (c *NacosNamingClient) GetAllInstances(serviceInfo ServiceInfo) ([]Address, error) {
	return c.httpSdk.GetAllInstances(vo.SelectAllInstancesParam{
		ServiceName: serviceInfo.ServiceName,
		GroupName:   serviceInfo.Group,
	})
}

func (c *NacosNamingClient) UnregisterService(serviceInfo ServiceInfo) bool {
	logger.Infof("Unregister service (%s@@%s).", serviceInfo.ServiceName, serviceInfo.Group)
	old, err := c.httpSdk.GetAllInstances(vo.SelectAllInstancesParam{
		ServiceName: serviceInfo.ServiceName,
		GroupName:   serviceInfo.Group,
	})

	if err != nil {
		logger.Errorf("Select all instances fail, service (%s@@%s).", serviceInfo.ServiceName, serviceInfo.Group)
		return false
	}

	return c.UnregisterServiceInstances(serviceInfo, old)
}

func (c *NacosNamingClient) RegisterServiceInstances(serviceInfo ServiceInfo, addresses []Address) bool {
	if !c.UpdateServiceHealthCheckType(serviceInfo.ServiceKey) {
		log.Log.Info(fmt.Sprintf("Update service health check type fail, service (%s@@%s) is not registered.", serviceInfo.ServiceName, serviceInfo.Group))
		return false
	}

	oldAddresses, err := c.GetAllInstances(serviceInfo)
	log.Log.Info("RegisterServiceInstances, old: " + strconv.Itoa(len(oldAddresses)))

	if err != nil {
		log.Log.Error(err, fmt.Sprintf("Select all instances fail, service (%s@@%s).", serviceInfo.ServiceName, serviceInfo.Group))
		return false
	}

	added, deleted := diffAddresses(oldAddresses, addresses)

	if len(added) > 0 || len(deleted) > 0 {
		log.Log.Info(fmt.Sprintf("Register service (%s@@%s), added %d, deleted %d.",
			serviceInfo.ServiceName, serviceInfo.Group, len(added), len(deleted)))
	}

	for _, address := range added {
		if _, err := c.client.RegisterInstance(vo.RegisterInstanceParam{
			Ip:          address.IP,
			Port:        address.Port,
			Weight:      DefaultNacosEndpointWeight,
			Enable:      true,
			Healthy:     true,
			Metadata:    serviceInfo.Metadata,
			ServiceName: serviceInfo.ServiceName,
			GroupName:   serviceInfo.Group,
			Ephemeral:   false,
		}); err != nil {
			log.Log.Error(err, fmt.Sprintf("Register instance (%s:%d) with service (%s@@%s) fail",
				address.IP, address.Port, serviceInfo.ServiceName, serviceInfo.Group))
			return false
		}
	}

	for _, address := range deleted {
		if _, err := c.client.DeregisterInstance(vo.DeregisterInstanceParam{
			Ip:          address.IP,
			Port:        address.Port,
			ServiceName: serviceInfo.ServiceName,
			GroupName:   serviceInfo.Group,
			Ephemeral:   false,
		}); err != nil {
			log.Log.Error(err, fmt.Sprintf("Unregister instance (%s:%d) with service (%s@@%s) fail",
				address.IP, address.Port, serviceInfo.ServiceName, serviceInfo.ServiceKey.Group))
			return false
		}
	}

	return true
}

func (c *NacosNamingClient) UnregisterServiceInstances(serviceInfo ServiceInfo, addresses []Address) bool {
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
			return false
		}
	}
	return true
}

func diffAddresses(old, curr []Address) ([]Address, []Address) {
	bs, _ := json.Marshal(old)
	bsNew, _ := json.Marshal(curr)
	log.Log.Info("diffAddresses, old: " + string(bs) + ", curr: " + string(bsNew))

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
