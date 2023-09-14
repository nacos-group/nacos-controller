package nacos

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"sync"
)

var (
	SecretGVK    = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	ConfigMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
)

var nacosConfigClientCache = sync.Map{}

type nacosClientParams struct {
	Endpoint   string
	ServerAddr string
	Namespace  string
	AuthInfo   nacosAuthInfo
}

type nacosAuthInfo struct {
	AccessKey string
	SecretKey string
}

const (
	secretAuthKeyAccessKey = "ak"
	secretAuthKeySecretKey = "sk"
)

func GetNacosConfigurationUniKey(namespace, group, dataId string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, group, dataId)
}

func getNacosClientParams(c client.Client, dc *nacosiov1.DynamicConfiguration) (*nacosClientParams, error) {
	if dc == nil {
		return nil, fmt.Errorf("empty DynamicConfiguration")
	}
	serverConf := &dc.Spec.NacosServer
	authInfo, err := getNacosAuthInfo(c, dc.Namespace, *serverConf.AuthRef)
	if err != nil {
		return nil, err
	}
	if serverConf.Endpoint != nil {
		return &nacosClientParams{
			Endpoint:  *serverConf.Endpoint,
			Namespace: serverConf.Namespace,
			AuthInfo:  *authInfo,
		}, nil
	}
	if serverConf.ServerAddr != nil {
		return &nacosClientParams{
			ServerAddr: *serverConf.ServerAddr,
			Namespace:  serverConf.Namespace,
			AuthInfo:   *authInfo,
		}, nil
	}
	return nil, fmt.Errorf("EdasNamespace / Endpoint / ServerAddr, one of them should be set")
}

func getNacosAuthInfo(c client.Client, namespace string, obj v1.ObjectReference) (*nacosAuthInfo, error) {
	switch obj.GroupVersionKind().String() {
	case SecretGVK.String():
		s := v1.Secret{}
		err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: obj.Name}, &s)
		if err != nil {
			return nil, err
		}
		info := nacosAuthInfo{}
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
	default:
		return nil, fmt.Errorf("unsupported nacos auth reference type: %s", obj.GroupVersionKind().String())
	}
}

func GetOrCreateNacosConfigClient(c client.Client, dc *nacosiov1.DynamicConfiguration) (config_client.IConfigClient, error) {
	if dc == nil {
		return nil, fmt.Errorf("empty DynamicConfiguration")
	}
	clientParams, err := getNacosClientParams(c, dc)
	if err != nil {
		return nil, err
	}
	// 简化判空逻辑，cacheKey仅内部使用
	cacheKey := fmt.Sprintf("%s-%s-%s", clientParams.Endpoint, clientParams.ServerAddr, clientParams.Namespace)
	cachedClient, ok := nacosConfigClientCache.Load(cacheKey)
	if ok && cachedClient != nil {
		return cachedClient.(config_client.IConfigClient), nil
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
	nacosConfigClientCache.Store(cacheKey, configClient)
	return configClient, nil
}

func CalcMd5(s string) string {
	if len(s) == 0 {
		return ""
	}
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func StringSliceContains(arr []string, item string) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}
