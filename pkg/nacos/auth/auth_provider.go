package auth

import (
	"context"
	"fmt"
	client2 "github.com/nacos-group/nacos-controller/pkg/nacos/client"

	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	secretAuthKeyAccessKey = "accessKey"
	secretAuthKeySecretKey = "secretKey"
	secretAuthKeyUsername  = "username"
	secretAuthKeyPassword  = "password"
)

var (
	secretGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
)

type ConfigClientParam struct {
	Endpoint   string
	ServerAddr string
	Namespace  string
	AuthInfo   ConfigClientAuthInfo
}

type NacosClientParam struct {
	Endpoint   string
	ServerAddr string
	Namespace  string
	AuthInfo   NacosClientAuthInfo
}

type ConfigClientAuthInfo struct {
	AccessKey string
	SecretKey string
	Username  string
	Password  string
}

type NacosClientAuthInfo struct {
	AccessKey string
	SecretKey string
}

type NacosAuthProvider interface {
	GetNacosClientParams(authRef *v1.ObjectReference, nacosServerParam client2.NacosServerParam, key types.NamespacedName) (*ConfigClientParam, error)
	GetNacosNamingClientParams(*nacosiov1.ServiceDiscovery) (*NacosClientParam, error)
}

type DefaultNacosAuthProvider struct {
	Client client.Client
}

func NewDefaultNacosAuthProvider(c client.Client) NacosAuthProvider {
	return &DefaultNacosAuthProvider{Client: c}
}

func (p *DefaultNacosAuthProvider) GetNacosClientParams(authRef *v1.ObjectReference, nacosServerParam client2.NacosServerParam, key types.NamespacedName) (*ConfigClientParam, error) {
	var authInfo = &ConfigClientAuthInfo{}
	if authRef != nil {
		authRef = authRef.DeepCopy()
		authRef.Namespace = key.Namespace
		var err error
		authInfo, err = p.getNacosAuthInfo(authRef)
		if err != nil {
			return nil, err
		}
	}
	if len(nacosServerParam.Endpoint) > 0 {
		return &ConfigClientParam{
			Endpoint:  nacosServerParam.Endpoint,
			Namespace: nacosServerParam.Namespace,
			AuthInfo:  *authInfo,
		}, nil
	}
	if len(nacosServerParam.ServerAddr) > 0 {
		return &ConfigClientParam{
			ServerAddr: nacosServerParam.ServerAddr,
			Namespace:  nacosServerParam.Namespace,
			AuthInfo:   *authInfo,
		}, nil
	}
	return nil, fmt.Errorf("either endpoint or serverAddr should be set")
}

func (p *DefaultNacosAuthProvider) GetNacosNamingClientParams(sd *nacosiov1.ServiceDiscovery) (*NacosClientParam, error) {
	if sd == nil {
		return nil, fmt.Errorf("empty ServiceDiscovery")
	}
	serverConf := &sd.Spec.NacosServer
	authRef := serverConf.AuthRef.DeepCopy()
	authRef.Namespace = sd.Namespace

	authInfo, err := p.getNacosNamingAuthInfo(authRef)

	if err != nil {
		fmt.Println("getNacosNamingClientParams, create empty NacosClientAuthInfo, err:", err)
		authInfo = &NacosClientAuthInfo{}
	}

	if serverConf.Endpoint != "" {
		return &NacosClientParam{
			Endpoint:  serverConf.Endpoint,
			Namespace: serverConf.Namespace,
			AuthInfo:  *authInfo,
		}, nil
	}

	if serverConf.ServerAddr != "" {
		return &NacosClientParam{
			ServerAddr: serverConf.ServerAddr,
			Namespace:  serverConf.Namespace,
			AuthInfo:   *authInfo,
		}, nil
	}

	return nil, fmt.Errorf("either endpoint or serverAddr should be set")
}

func (p *DefaultNacosAuthProvider) getNacosAuthInfo(obj *v1.ObjectReference) (*ConfigClientAuthInfo, error) {
	switch obj.GroupVersionKind().String() {
	case secretGVK.String():
		return p.getNaocsAuthFromSecret(obj)
	default:
		return nil, fmt.Errorf("unsupported nacos auth reference type: %s", obj.GroupVersionKind().String())
	}
}

func (p *DefaultNacosAuthProvider) getNacosNamingAuthInfo(obj *v1.ObjectReference) (*NacosClientAuthInfo, error) {
	switch obj.GroupVersionKind().String() {
	case secretGVK.String():
		if nacosAuthInfo, err := p.getNacosAuthInfo(obj); err == nil {
			return &NacosClientAuthInfo{
				AccessKey: nacosAuthInfo.AccessKey,
				SecretKey: nacosAuthInfo.SecretKey,
			}, nil
		} else {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported nacos auth reference type: %s", obj.GroupVersionKind().String())
	}
}

func (p *DefaultNacosAuthProvider) getNaocsAuthFromSecret(obj *v1.ObjectReference) (*ConfigClientAuthInfo, error) {
	s := v1.Secret{}
	info := ConfigClientAuthInfo{}
	if err := p.Client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, &s); err != nil {
		if errors.IsNotFound(err) {
			return &info, nil
		}
		return nil, err
	}
	if v, ok := s.Data[secretAuthKeyAccessKey]; ok && len(v) > 0 {
		info.AccessKey = string(v)
	}
	if v, ok := s.Data[secretAuthKeySecretKey]; ok && len(v) > 0 {
		info.SecretKey = string(v)
	}
	if v, ok := s.Data[secretAuthKeyUsername]; ok && len(v) > 0 {
		info.Username = string(v)
	}
	if v, ok := s.Data[secretAuthKeyPassword]; ok && len(v) > 0 {
		info.Password = string(v)
	}
	return &info, nil
}
