package nacos

import (
	"context"
	"fmt"
	"github.com/nacos-group/nacos-controller/internal"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func init() {
	// Using ConfigMapWrapper as default ConfigMap resource wrapper
	RegisterObjectWrapperIfAbsent(ConfigMapGVK.String(), func(cs *kubernetes.Clientset, owner client.Object, objRef *v1.ObjectReference) (ObjectReferenceWrapper, error) {
		return &ConfigMapWrapper{
			cs:        cs,
			ObjectRef: objRef,
			owner:     owner,
		}, nil
	})
}

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

type NewObjectWrapperFn func(*kubernetes.Clientset, client.Object, *v1.ObjectReference) (ObjectReferenceWrapper, error)

var objectWrapperMap = map[string]NewObjectWrapperFn{}

func RegisterObjectWrapperIfAbsent(targetGVK string, fn NewObjectWrapperFn) {
	_, exist := objectWrapperMap[targetGVK]
	if exist {
		return
	}
	objectWrapperMap[targetGVK] = fn
}

func RegisterObjectWrapper(targetGVK string, fn NewObjectWrapperFn) {
	objectWrapperMap[targetGVK] = fn
}

func NewObjectReferenceWrapper(cs *kubernetes.Clientset, owner client.Object, objRef *v1.ObjectReference) (ObjectReferenceWrapper, error) {
	targetGVK := objRef.GroupVersionKind().String()
	fn, exist := objectWrapperMap[targetGVK]
	if !exist {
		return nil, fmt.Errorf("unsupport object reference type: %s", targetGVK)
	}
	return fn(cs, owner, objRef)
}

type ConfigMapWrapper struct {
	ObjectRef *v1.ObjectReference
	cm        *v1.ConfigMap
	owner     client.Object
	cs        *kubernetes.Clientset
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
	var err error
	var cm *v1.ConfigMap
	if cm, err = cmw.cs.CoreV1().ConfigMaps(cmw.cm.Namespace).Update(context.TODO(), cmw.cm, metav1.UpdateOptions{}); err != nil {
		return err
	}
	cmw.cm = cm
	return nil
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
	var cm *v1.ConfigMap
	var err error

	if cm, err = cmw.cs.CoreV1().ConfigMaps(cmw.ObjectRef.Namespace).Get(context.TODO(), cmw.ObjectRef.Name, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// if not found, try to create object reference
		cm = &v1.ConfigMap{}
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
		if cm, err = cmw.cs.CoreV1().ConfigMaps(cm.Namespace).Create(context.TODO(), cm, metav1.CreateOptions{}); err != nil {
			return err
		}
	}
	cmw.cm = cm
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
