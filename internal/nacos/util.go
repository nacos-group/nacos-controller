package nacos

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	ConfigMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
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
