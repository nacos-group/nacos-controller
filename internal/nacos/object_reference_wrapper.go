package nacos

import (
	"context"
	"fmt"
	"github.com/nacos-group/nacos-controller/internal"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

type ObjectReferenceWrapper interface {
	GetContent(dataId string) (string, bool, error)
	StoreContent(dataId string, content string) error
	InjectLabels(map[string]string)
	Flush() error
	Reload() error
}

func NewObjectReferenceWrapper(c client.Client, owner client.Object, objRef v1.ObjectReference) (ObjectReferenceWrapper, error) {
	switch objRef.GroupVersionKind().String() {
	case ConfigMapGVK.String():
		return &ConfigMapWrapper{
			Client:    c,
			ObjectRef: objRef,
			owner:     owner,
		}, nil
	default:
		return nil, fmt.Errorf("unsupport object reference type: %s", objRef.GroupVersionKind().String())
	}
}

type ConfigMapWrapper struct {
	ObjectRef v1.ObjectReference
	cm        *v1.ConfigMap
	owner     client.Object
	client.Client
}

func (cmw *ConfigMapWrapper) GetContent(dataId string) (string, bool, error) {
	if cmw.cm == nil {
		if err := cmw.Reload(); err != nil {
			return "", false, err
		}
	}
	data := cmw.cm.Data
	binaryData := cmw.cm.BinaryData
	if v, ok := data[dataId]; ok {
		return v, true, nil
	}
	if v, ok := binaryData[dataId]; ok {
		return string(v), true, nil
	}
	return "", false, nil
}

func (cmw *ConfigMapWrapper) StoreContent(dataId string, content string) error {
	if cmw.cm == nil {
		if err := cmw.Reload(); err != nil {
			return err
		}
	}
	// json 直接存储在data下不合法，需要存储在binaryData中
	if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") {
		if cmw.cm.BinaryData == nil {
			cmw.cm.BinaryData = map[string][]byte{}
		}
		cmw.cm.BinaryData[dataId] = []byte(content)
	} else {
		if cmw.cm.Data == nil {
			cmw.cm.Data = map[string]string{}
		}
		cmw.cm.Data[dataId] = content
	}
	return nil
}

func (cmw *ConfigMapWrapper) Flush() error {
	if cmw.cm == nil {
		return nil
	}
	return cmw.Update(context.TODO(), cmw.cm)
}

func (cmw *ConfigMapWrapper) InjectLabels(labels map[string]string) {
	if cmw.cm == nil {
		if err := cmw.Reload(); err != nil {
			return
		}
	}
	if len(labels) == 0 {
		return
	}
	if cmw.cm.Labels == nil {
		cmw.cm.Labels = map[string]string{}
	}
	for k, v := range labels {
		cmw.cm.Labels[k] = v
	}
}

func (cmw *ConfigMapWrapper) Reload() error {
	cm := v1.ConfigMap{}
	if err := cmw.Get(context.TODO(), types.NamespacedName{Namespace: cmw.ObjectRef.Namespace, Name: cmw.ObjectRef.Name}, &cm); err != nil {
		return err
	}
	cmw.cm = &cm
	return cmw.ensureOwnerLabel()
}

func (cmw *ConfigMapWrapper) ensureOwnerLabel() error {
	if cmw.cm == nil {
		return nil
	}
	if cmw.cm.Labels == nil {
		cmw.cm.Labels = map[string]string{}
	}
	if v, ok := cmw.cm.Labels[internal.ConfigMapLabel]; !ok || v != cmw.owner.GetName() {
		cmw.cm.Labels[internal.ConfigMapLabel] = cmw.owner.GetName()
		return cmw.Flush()
	}
	return nil
}
