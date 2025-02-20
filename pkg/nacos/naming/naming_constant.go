package naming

import "time"

const (
	DefaultTaskDelay = 1 * time.Second

	DefaultResyncInterval = 0

	DefaultNacosEndpointWeight = 100

	MaxRetry = 3
	//
	//ToNacos Direction = "to-nacos"
	//
	//ToK8s Direction = "to-k8s"
	//
	//Both Direction = "both"

	NamingSyncedMark = "synced_by_nacos_controller"

	NamingDefaultGroupName string = "DEFAULT_GROUP"
)
