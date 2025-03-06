package nacos

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ConfigMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	SecretGVK    = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
)

func GetNacosConfigurationUniKey(namespace, group, dataId string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, group, dataId)
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

func DynamicConfigurationMatch(object client.Object, dc *nacosiov1.DynamicConfiguration) bool {
	if object.GetNamespace() != dc.Namespace {
		return false
	}
	if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopeFull {
		return true
	}
	_, kind := object.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
	for _, ref := range dc.Spec.ObjectRefs {
		if ref.Kind == kind && ref.Name == object.GetName() {
			return true
		}
	}
	return true
}

func GetAllKeys(object client.Object) []string {
	switch object.GetObjectKind().GroupVersionKind().Kind {
	case "ConfigMap":
		return GetConfigMapAllKeys(object.(*v1.ConfigMap))
	case "Secret":
		return GetSecretAllKeys(object.(*v1.Secret))
	default:
		return []string{}
	}
}

func GetConfigMapAllKeys(configMap *v1.ConfigMap) []string {
	data := configMap.Data
	binaryData := configMap.BinaryData
	var keys []string
	for key := range data {
		keys = append(keys, key)
	}
	for key := range binaryData {
		keys = append(keys, key)
	}
	return keys
}

func GetSecretAllKeys(secret *v1.Secret) []string {
	var keys []string
	for key := range secret.Data {
		keys = append(keys, key)
	}
	return keys
}

func GetContent(object client.Object, dataId string) (string, bool) {
	switch object.GetObjectKind().GroupVersionKind().Kind {
	case "ConfigMap":
		return GetConfigMapContent(object.(*v1.ConfigMap), dataId)
	case "Secret":
		return GetSecretContent(object.(*v1.Secret), dataId)
	default:
		return "", false
	}
}

func GetConfigMapContent(configMap *v1.ConfigMap, dataId string) (string, bool) {
	data := configMap.Data
	binaryData := configMap.BinaryData
	if v, ok := data[dataId]; ok {
		return v, true
	}
	if v, ok := binaryData[dataId]; ok {
		return string(v), true
	}
	return "", false
}

func GetSecretContent(secret *v1.Secret, dataId string) (string, bool) {
	data := secret.Data
	if v, ok := data[dataId]; ok {
		return base64.StdEncoding.EncodeToString(v), true
		//return string(v), true
	}
	return "", false
}

func StoreContent(object client.Object, dataId string, content string) error {
	switch object.GetObjectKind().GroupVersionKind().Kind {
	case "ConfigMap":
		return StoreConfigMapContent(object.(*v1.ConfigMap), dataId, content)
	case "Secret":
		return StoreSecretContent(object.(*v1.Secret), dataId, content)
	default:
		return nil
	}
}

func StoreConfigMapContent(configMap *v1.ConfigMap, dataId string, content string) error {
	if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") {
		if configMap.BinaryData == nil {
			configMap.BinaryData = map[string][]byte{}
		}
		configMap.BinaryData[dataId] = []byte(content)
	} else {
		if configMap.Data == nil {
			configMap.Data = map[string]string{}
		}
		configMap.Data[dataId] = content
	}
	return nil
}

func StoreSecretContent(secret *v1.Secret, dataId string, content string) error {
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	base64content, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return err
	}
	secret.Data[dataId] = []byte(base64content)
	return nil
}

func CompareDataIds(listenDataIds []string, localDataIds []string) ([]string, []string, []string) {
	listenOnly := make(map[string]bool)
	localOnly := make(map[string]bool)
	allIds := make(map[string]bool)

	// Mark all IDs from listenDataIds
	for _, id := range listenDataIds {
		listenOnly[id] = true

	}

	// Mark all IDs from localDataIds and find common IDs
	for _, id := range localDataIds {
		if listenOnly[id] {
			delete(listenOnly, id)
			allIds[id] = true
		} else {
			localOnly[id] = true
		}
	}

	// Collect IDs that are only in listenDataIds
	listenOnlySlice := make([]string, 0, len(listenOnly))
	for id := range listenOnly {
		listenOnlySlice = append(listenOnlySlice, id)
	}

	// Collect IDs that are only in localDataIds
	localOnlySlice := make([]string, 0, len(localOnly))
	for id := range localOnly {
		localOnlySlice = append(localOnlySlice, id)
	}

	bothSlice := make([]string, 0, len(allIds))
	for id := range allIds {
		bothSlice = append(bothSlice, id)
	}

	return listenOnlySlice, localOnlySlice, bothSlice
}
