package nacos

import (
	"context"
	"fmt"
	"strings"

	"github.com/nacos-group/nacos-controller/pkg"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	RegisterObjectWrapperIfAbsent(SecretGVK.String(), func(cs *kubernetes.Clientset, owner client.Object, objRef *v1.ObjectReference) (ObjectReferenceWrapper, error) {
		return &SecretWrapper{
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

	StoreContentLatest(dataId string, content string) error

	GetContentLatest(dataId string) (string, bool, error)

	GetAllKeys() ([]string, error)
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

func (cmw *ConfigMapWrapper) GetContentLatest(dataId string) (string, bool, error) {
	var cm *v1.ConfigMap
	var err error
	if cm, err = cmw.cs.CoreV1().ConfigMaps(cmw.ObjectRef.Namespace).Get(context.TODO(), cmw.ObjectRef.Name, metav1.GetOptions{}); err != nil {
		return "", false, err
	}
	data := cm.Data
	binaryData := cm.BinaryData
	cmw.cm = cm
	if v, ok := data[dataId]; ok {
		return v, true, nil
	}
	if v, ok := binaryData[dataId]; ok {
		return string(v), true, nil
	}
	return "", false, nil
}

func (cmw *ConfigMapWrapper) GetAllKeys() ([]string, error) {
	if cmw.cm == nil {
		if err := cmw.Reload(); err != nil {
			return nil, err
		}
	}
	data := cmw.cm.Data
	binaryData := cmw.cm.BinaryData
	var keys []string
	for key := range data {
		keys = append(keys, key)
	}
	for key := range binaryData {
		keys = append(keys, key)
	}
	return keys, nil
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

func (cmw *ConfigMapWrapper) StoreContentLatest(dataId string, content string) error {
	var cm *v1.ConfigMap
	var err error
	var newflag bool

	if cm, err = cmw.cs.CoreV1().ConfigMaps(cmw.ObjectRef.Namespace).Get(context.TODO(), cmw.ObjectRef.Name, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			return err
		} else {
			newflag = true
		}
		// if not found, try to create object reference
		cm = &v1.ConfigMap{}
		cm.Namespace = cmw.ObjectRef.Namespace
		cm.Name = cmw.ObjectRef.Name
	}

	if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") {
		if cm.BinaryData == nil {
			cm.BinaryData = map[string][]byte{}
		}
		cm.BinaryData[dataId] = []byte(content)
	} else {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[dataId] = content
	}
	if !controllerutil.ContainsFinalizer(cm, pkg.FinalizerName) {
		controllerutil.AddFinalizer(cm, pkg.FinalizerName)
	}
	if newflag {
		if cm, err = cmw.cs.CoreV1().ConfigMaps(cm.Namespace).Create(context.TODO(), cm, metav1.CreateOptions{}); err != nil {
			return err
		}
	} else if cm, err = cmw.cs.CoreV1().ConfigMaps(cm.Namespace).Update(context.TODO(), cm, metav1.UpdateOptions{}); err != nil {
		return err
	}
	cmw.cm = cm
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
		if cm, err = cmw.cs.CoreV1().ConfigMaps(cm.Namespace).Create(context.TODO(), cm, metav1.CreateOptions{}); err != nil {
			return err
		}
	}
	if !controllerutil.ContainsFinalizer(cm, pkg.FinalizerName) {
		controllerutil.AddFinalizer(cm, pkg.FinalizerName)
		if cm, err = cmw.cs.CoreV1().ConfigMaps(cm.Namespace).Update(context.TODO(), cm, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	cmw.cm = cm
	return nil
}

type SecretWrapper struct {
	ObjectRef *v1.ObjectReference
	secret    *v1.Secret
	owner     client.Object
	cs        *kubernetes.Clientset
}

func (sw *SecretWrapper) GetContent(dataId string) (string, bool, error) {
	if sw.secret == nil {
		if err := sw.Reload(); err != nil {
			return "", false, err
		}
	}
	content, ifExist := GetSecretContent(sw.secret, dataId)
	return content, ifExist, nil
}

func (sw *SecretWrapper) GetContentLatest(dataId string) (string, bool, error) {
	var secret *v1.Secret
	var err error

	if secret, err = sw.cs.CoreV1().Secrets(sw.ObjectRef.Namespace).Get(context.TODO(), sw.ObjectRef.Name, metav1.GetOptions{}); err != nil {
		return "", false, err
	}
	content, ifExist := GetSecretContent(secret, dataId)
	sw.secret = secret
	return content, ifExist, nil
}

func (sw *SecretWrapper) GetAllKeys() ([]string, error) {
	if sw.secret == nil {
		if err := sw.Reload(); err != nil {
			return nil, err
		}
	}
	return GetSecretAllKeys(sw.secret), nil
}

func (sw *SecretWrapper) StoreContent(dataId string, content string) error {
	if sw.secret == nil {
		if err := sw.Reload(); err != nil {
			return err
		}
	}
	err := StoreSecretContent(sw.secret, dataId, content)
	if err != nil {
		return err
	}
	return nil
}

func (sw *SecretWrapper) StoreContentLatest(dataId string, content string) error {
	var secret *v1.Secret
	var err error
	var newflag bool

	if secret, err = sw.cs.CoreV1().Secrets(sw.ObjectRef.Namespace).Get(context.TODO(), sw.ObjectRef.Name, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			return err
		} else {
			newflag = true
		}
		secret = &v1.Secret{}
		secret.Namespace = sw.ObjectRef.Namespace
		secret.Name = sw.ObjectRef.Name
	}
	err = StoreSecretContent(secret, dataId, content)
	if err != nil {
		return err
	}
	if !controllerutil.ContainsFinalizer(secret, pkg.FinalizerName) {
		controllerutil.AddFinalizer(secret, pkg.FinalizerName)
	}

	if newflag {
		if secret, err = sw.cs.CoreV1().Secrets(secret.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			return err
		}
	} else if secret, err = sw.cs.CoreV1().Secrets(secret.Namespace).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
		return err
	}
	sw.secret = secret
	return nil
}

func (sw *SecretWrapper) DeleteContent(dataId string) error {
	if sw.secret == nil {
		if err := sw.Reload(); err != nil {
			return err
		}
	}
	if _, ok := sw.secret.Data[dataId]; ok {
		delete(sw.secret.Data, dataId)
		return nil
	}
	return nil
}

func (sw *SecretWrapper) StoreAllContent(dataMap map[string]string) (bool, error) {
	changed := false
	for dataId, newContent := range dataMap {
		if !changed {
			if oldContent, exist, err := sw.GetContent(dataId); err != nil {
				return false, err
			} else if !exist || CalcMd5(newContent) != CalcMd5(oldContent) {
				changed = true
			}
		}
		if err := sw.StoreContent(dataId, newContent); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func (sw *SecretWrapper) Flush() error {
	if sw.secret == nil {
		return nil
	}
	var err error
	var secret *v1.Secret
	if secret, err = sw.cs.CoreV1().Secrets(sw.secret.Namespace).Update(context.TODO(), sw.secret, metav1.UpdateOptions{}); err != nil {
		return err
	}
	sw.secret = secret
	return nil
}

func (sw *SecretWrapper) InjectLabels(labels map[string]string) {
	if sw.secret == nil {
		if err := sw.Reload(); err != nil {
			return
		}
	}
	if len(labels) == 0 {
		return
	}
	if sw.secret.Labels == nil {
		sw.secret.Labels = map[string]string{}
	}
	for k, v := range labels {
		sw.secret.Labels[k] = v
	}
}

func (sw *SecretWrapper) Reload() error {
	var secret *v1.Secret
	var err error

	if secret, err = sw.cs.CoreV1().Secrets(sw.ObjectRef.Namespace).Get(context.TODO(), sw.ObjectRef.Name, metav1.GetOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		secret = &v1.Secret{}
		secret.Namespace = sw.ObjectRef.Namespace
		secret.Name = sw.ObjectRef.Name
		if secret, err = sw.cs.CoreV1().Secrets(secret.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			return err
		}
	}
	if !controllerutil.ContainsFinalizer(secret, pkg.FinalizerName) {
		controllerutil.AddFinalizer(secret, pkg.FinalizerName)
		if secret, err = sw.cs.CoreV1().Secrets(secret.Namespace).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	sw.secret = secret
	return nil
}
