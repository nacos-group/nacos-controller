package nacos

import (
	"context"
	"fmt"
	"github.com/nacos-group/nacos-controller/internal"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

type ObjectReferenceWrapper interface {
	//GetContent return content by dataId
	GetContent(dataId string) (string, bool, error)
	//StoreContent store content by dataId, return err if happened
	StoreContent(dataId string, content string) error
	//StoreAllContent store a map with dataId as key and content as value, return true if any content changed and error
	StoreAllContent(map[string]string) (bool, error)
	//DeleteContent remove a dataId
	DeleteContent(dataId string) error
	//InjectLabels to ObjectReference
	InjectLabels(map[string]string)
	//Flush write all content to ObjectReference
	Flush() error
	//Reload load ObjectReference, create it when not found
	Reload() error
}

func NewObjectReferenceWrapper(c client.Client, owner client.Object, objRef *v1.ObjectReference) (ObjectReferenceWrapper, error) {
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
	ObjectRef *v1.ObjectReference
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

func (cmw *ConfigMapWrapper) DeleteContent(dataId string) error {
	if cmw.cm == nil {
		if err := cmw.Reload(); err != nil {
			return err
		}
	}
	if _, ok := cmw.cm.Data[dataId]; ok {
		delete(cmw.cm.Data, dataId)
		return nil
	}
	if _, ok := cmw.cm.BinaryData[dataId]; ok {
		delete(cmw.cm.Data, dataId)
		return nil
	}
	return nil
}

func (cmw *ConfigMapWrapper) StoreAllContent(dataMap map[string]string) (bool, error) {
	changed := false
	for dataId, newContent := range dataMap {
		if !changed {
			if oldContent, exist, err := cmw.GetContent(dataId); err != nil {
				return false, err
			} else if !exist || CalcMd5(newContent) != CalcMd5(oldContent) {
				changed = true
			}
		}
		if err := cmw.StoreContent(dataId, newContent); err != nil {
			return false, err
		}
	}
	return changed, nil
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
		if errors.IsNotFound(err) {
			// if not found, try to create object reference
			cm.Namespace = cmw.ObjectRef.Namespace
			cm.Name = cmw.ObjectRef.Name

			owner := cmw.owner
			apiVersion, kind := owner.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
			cm.SetOwnerReferences([]v12.OwnerReference{
				{
					APIVersion:         apiVersion,
					Kind:               kind,
					Name:               owner.GetName(),
					UID:                owner.GetUID(),
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			})
			if err := cmw.Create(context.TODO(), &cm); err != nil {
				return err
			}
		} else {
			return err
		}
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
