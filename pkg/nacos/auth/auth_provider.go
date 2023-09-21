package auth

import (
	"context"
	"fmt"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	secretAuthKeyAccessKey = "ak"
	secretAuthKeySecretKey = "sk"
)

var (
	secretGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
)

type NacosAuthProvider interface {
	GetNacosClientParams(*nacosiov1.DynamicConfiguration) (*ConfigClientParam, error)
}

type DefaultNaocsAuthProvider struct {
	client.Client
}

func (p *DefaultNaocsAuthProvider) GetNacosClientParams(dc *nacosiov1.DynamicConfiguration) (*ConfigClientParam, error) {
	if dc == nil {
		return nil, fmt.Errorf("empty DynamicConfiguration")
	}
	serverConf := &dc.Spec.NacosServer
	authRef := serverConf.AuthRef.DeepCopy()
	authRef.Namespace = dc.Namespace

	authInfo, err := p.getNacosAuthInfo(authRef)
	if err != nil {
		return nil, err
	}
	if serverConf.Endpoint != nil {
		return &ConfigClientParam{
			Endpoint:  *serverConf.Endpoint,
			Namespace: serverConf.Namespace,
			AuthInfo:  *authInfo,
		}, nil
	}
	if serverConf.ServerAddr != nil {
		return &ConfigClientParam{
			ServerAddr: *serverConf.ServerAddr,
			Namespace:  serverConf.Namespace,
			AuthInfo:   *authInfo,
		}, nil
	}
	return nil, fmt.Errorf("either endpoint or serverAddr should be set")
}

func (p *DefaultNaocsAuthProvider) getNacosAuthInfo(obj *v1.ObjectReference) (*ConfigClientAuthInfo, error) {
	switch obj.GroupVersionKind().String() {
	case secretGVK.String():
		return p.getNaocsAuthFromSecret(obj)
	default:
		return nil, fmt.Errorf("unsupported nacos auth reference type: %s", obj.GroupVersionKind().String())
	}
}

func (p *DefaultNaocsAuthProvider) getNaocsAuthFromSecret(obj *v1.ObjectReference) (*ConfigClientAuthInfo, error) {
	s := v1.Secret{}
	err := p.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, &s)
	if err != nil {
		return nil, err
	}
	info := ConfigClientAuthInfo{}
	if v, ok := s.Data[secretAuthKeyAccessKey]; ok && len(v) > 0 {
		info.AccessKey = string(v)
	} else {
		return nil, fmt.Errorf("empty field %s in secret %s", secretAuthKeyAccessKey, obj.Name)
	}
	if v, ok := s.Data[secretAuthKeySecretKey]; ok && len(v) > 0 {
		info.SecretKey = string(v)
	} else {
		return nil, fmt.Errorf("empty field %s in secret %s", secretAuthKeySecretKey, obj.Name)
	}
	return &info, nil
}
