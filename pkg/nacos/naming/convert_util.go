package naming

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nacos-group/nacos-sdk-go/v2/common/logger"
	v1 "k8s.io/api/core/v1"
)

const (
	// annotationServiceSync is the key of the annotation that determines
	// whether to sync the Service resource or not.
	annotationServiceSync = "nacos.io/service-sync"

	// annotationServiceName is set to override the name of the service
	// registered.
	annotationServiceName = "nacos.io/service-name"

	// annotationServiceGroup is set to override the group of the service
	// registered.
	annotationServiceGroup = "nacos.io/service-group"

	// annotationServicePort specifies the port to use as the service instance
	// port when registering a service. This can be a named port in the
	// service or an integer value.
	annotationServicePort = "nacos.io/service-port"

	// annotationServiceMeta specifies the meta of nacos service.
	// The format must be json.
	annotationServiceMeta = "nacos.io/service-meta"
)

func ShouldServiceSync(svc *v1.Service) bool {
	raw, ok := svc.Annotations[annotationServiceSync]
	if !ok {
		return false
	}

	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}

	return v
}

func ConvertToAddresses(endpoints *v1.Endpoints, serviceInfo ServiceInfo) []Address {
	addresses := make([]Address, 0)
	bytes, _ := json.Marshal(endpoints)

	fmt.Println("endpoints: ", string(bytes))
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if serviceInfo.Port > 0 {
				addresses = append(addresses, Address{
					IP:   address.IP,
					Port: uint64(serviceInfo.Port),
				})
			} else {
				addresses = append(addresses, Address{
					IP:   address.IP,
					Port: uint64(subset.Ports[0].Port),
				})
			}
		}
	}
	bytes, _ = json.Marshal(addresses)
	fmt.Println("address: ", string(bytes))
	return addresses
}

func GetEndpointPort(ep *v1.EndpointPort) {
	logger.Info("Get endpoint port")
}

func GenerateServiceInfo(svc *v1.Service) (ServiceInfo, error) {
	serviceName := svc.Annotations[annotationServiceName]
	if serviceName == "" {
		// fall back to get the name of service resource
		logger.Info("The service name annotion is empty, so we use the name of service resource.")
		serviceName = svc.Name
	}

	port, err := strconv.ParseUint(svc.Annotations[annotationServicePort], 0, 0)
	if err != nil {
		logger.Info("Failed to parse the service's port, caused: " + err.Error())
		port = 0
	}

	meta := make(map[string]string)
	rawMeta := svc.Annotations[annotationServiceMeta]
	if rawMeta != "" {
		if err := json.Unmarshal([]byte(svc.Annotations[annotationServiceMeta]), &meta); err != nil {
			logger.Info("Failed to parse the service's meta, caused: " + err.Error() + ", raw meta: " + rawMeta)
			return ServiceInfo{}, err
		}
	}

	for k, v := range svc.Annotations {
		if !strings.HasPrefix(k, "nacos.io/") {
			meta[k] = v
		}
	}

	// We need to mark the service is synced by nacos controller
	meta[NamingSyncedMark] = "true"

	groupName := svc.Annotations[annotationServiceGroup]

	if groupName == "" {
		groupName = NamingDefaultGroupName
	}

	// Now we only trust the annotations.
	// TODO Extract value from the spec of service resource for extended features
	return ServiceInfo{
		ServiceKey: ServiceKey{
			ServiceName: serviceName,
			Group:       groupName,
		},
		Port:     port,
		Metadata: meta,
	}, nil
}
