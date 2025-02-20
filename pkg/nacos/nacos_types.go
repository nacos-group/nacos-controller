package nacos

import (
	nacoscontrollerv1 "github.com/nacos-group/nacos-controller/api/v1"
	v1 "k8s.io/api/core/v1"
)

type NacosServer interface {
	GetServerConf() (nacoscontrollerv1.NacosServerConfiguration, error)
	GetAuthRef() (v1.ObjectReference, error)
}
