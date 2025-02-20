package nacos

import (
	"context"
	"fmt"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
	"time"
)

type Server2ClusterCallback interface {
	Callback(namespace, group, dataId, content string)
	CallbackWithContext(ctx context.Context, namespace, group, dataId, content string)
}

func NewDefaultServer2ClusterCallback(c client.Client, cs *kubernetes.Clientset, mappings *DataId2DCMappings, locks *LockManager) Server2ClusterCallback {
	return &DefaultServer2ClusterCallback{
		Client:   c,
		cs:       cs,
		mappings: mappings,
		locks:    locks,
	}
}

type DefaultServer2ClusterCallback struct {
	client.Client
	cs       *kubernetes.Clientset
	mappings *DataId2DCMappings
	locks    *LockManager
}

func (cb *DefaultServer2ClusterCallback) Callback(namespace, group, dataId, content string) {
	cb.CallbackWithContext(context.Background(), namespace, group, dataId, content)
}

func (cb *DefaultServer2ClusterCallback) CallbackWithContext(ctx context.Context, namespace, group, dataId, content string) {
	l := log.FromContext(ctx, "namespace", namespace, "group", group, "dataId", dataId)
	ctx = log.IntoContext(ctx, l)
	l.Info("server2cluster callback")
	dcNNList := cb.mappings.GetDCList(namespace, group, dataId)
	for _, nn := range dcNNList {
		if err := retry.RetryOnConflict(wait.Backoff{
			Duration: 1 * time.Second,
			Factor:   2,
			Steps:    3,
		}, func() error {
			// 以DC纬度，锁住更新
			l.Info("server2cluster callback for " + nn.String())
			lockName := nn.String()
			lock := cb.locks.GetLock(lockName)
			lock.Lock()
			defer lock.Unlock()
			return cb.server2ClusterCallbackOneDC(ctx, namespace, group, dataId, content, nn)
		}); err != nil {
			l.Error(err, "update config failed", "dc", nn)
		}
	}
	l.Info("server2cluster callback processed", "dcList", dcNNList)
}

func (cb *DefaultServer2ClusterCallback) server2ClusterCallbackOneDC(ctx context.Context, namespace, group, dataId, content string, nn types.NamespacedName) error {
	l := log.FromContext(ctx)
	l = l.WithValues("dc", nn)
	dc := nacosiov1.DynamicConfiguration{}
	if err := cb.Get(ctx, nn, &dc); err != nil {
		if errors.IsNotFound(err) {
			cb.mappings.RemoveMapping(namespace, group, dataId, nn)
			l.Info("mapping removed due to dc not found")
			return nil
		}
		l.Error(err, "get DynamicConfiguration error")
		return err
	}
	if !StringSliceContains(dc.Spec.DataIds, dataId) {
		cb.mappings.RemoveMapping(namespace, group, dataId, nn)
		l.Info("mapping removed due to dataId not found in spec.DataIds")
		return nil
	}
	if dc.Spec.NacosServer.Namespace != namespace {
		cb.mappings.RemoveMapping(namespace, group, dataId, nn)
		l.Info("mapping removed due to namespace changed", "namespace from server", namespace, "namespace in dc", dc.Spec.NacosServer.Namespace)
		return nil
	}
	if dc.Spec.NacosServer.Group != group {
		cb.mappings.RemoveMapping(namespace, group, dataId, nn)
		l.Info("mapping removed due to group changed", "group from server", group, "group in dc", dc.Spec.NacosServer.Group)
		return nil
	}
	if dc.Status.ObjectRef == nil {
		err := fmt.Errorf("ObjectReference empty in status")
		l.Error(err, "ObjectReference empty in status")
		return err
	}

	objRef := dc.Status.ObjectRef.DeepCopy()
	objRef.Namespace = dc.Namespace
	objWrapper, err := NewObjectReferenceWrapper(cb.cs, &dc, objRef)
	if err != nil {
		l.Error(err, "create object wrapper error", "objRef", objRef)
		return err
	}
	oldContent, _, err := objWrapper.GetContent(dataId)
	if err != nil {
		l.Error(err, "read content error")
		return err
	}
	newMd5 := CalcMd5(content)
	if newMd5 == CalcMd5(oldContent) {
		l.Info("ignored due to same content", "md5", newMd5)
		UpdateSyncStatusIfAbsent(&dc, dataId, newMd5, "server", metav1.Now(), true, "skipped due to same md5")
		return nil
	}
	if err := objWrapper.StoreContent(dataId, content); err != nil {
		l.Error(err, "update content error", "obj", objRef)
		return err
	}
	UpdateSyncStatus(&dc, dataId, newMd5, "server", metav1.Now(), true, "")
	return cb.Status().Update(ctx, &dc)
}

type DataId2DCMappings struct {
	m    map[string][]types.NamespacedName
	lock sync.RWMutex
}

func NewDataId2DCMappings() *DataId2DCMappings {
	return &DataId2DCMappings{
		m:    map[string][]types.NamespacedName{},
		lock: sync.RWMutex{},
	}
}

func (d *DataId2DCMappings) AddMapping(namespaceId, group, dataId string, nn types.NamespacedName) {
	d.lock.Lock()
	defer d.lock.Unlock()
	key := GetNacosConfigurationUniKey(namespaceId, group, dataId)
	arr, ok := d.m[key]
	if !ok {
		d.m[key] = []types.NamespacedName{nn}
		return
	}
	found := false
	for _, v := range arr {
		if v.String() == nn.String() {
			found = true
			break
		}
	}
	if found {
		return
	}
	arr = append(arr, nn)
	d.m[key] = arr
}

func (d *DataId2DCMappings) GetDCList(namespaceId, group, dataId string) []types.NamespacedName {
	d.lock.RLock()
	d.lock.RUnlock()

	key := GetNacosConfigurationUniKey(namespaceId, group, dataId)
	return d.m[key]
}

func (d *DataId2DCMappings) HasMapping(namespaceId, group, dataId string, nn types.NamespacedName) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()

	key := GetNacosConfigurationUniKey(namespaceId, group, dataId)
	arr, ok := d.m[key]
	if !ok {
		return false
	}
	for _, v := range arr {
		if v.String() == nn.String() {
			return true
		}
	}
	return false
}

func (d *DataId2DCMappings) RemoveMapping(namespaceId, group, dataId string, nn types.NamespacedName) {
	if !d.HasMapping(namespaceId, group, dataId, nn) {
		return
	}

	d.lock.Lock()
	defer d.lock.Unlock()

	key := GetNacosConfigurationUniKey(namespaceId, group, dataId)
	arr := d.m[key]
	var newArr []types.NamespacedName
	for _, v := range arr {
		if v.String() == nn.String() {
			continue
		}
		newArr = append(newArr, v)
	}
	d.m[key] = newArr
}

type LockManager struct {
	locks map[string]*sync.Mutex
	lock  sync.RWMutex
}

func NewLockManager() *LockManager {
	return &LockManager{
		locks: map[string]*sync.Mutex{},
		lock:  sync.RWMutex{},
	}
}

func (lm *LockManager) GetLock(key string) *sync.Mutex {
	if !lm.HasLock(key) {
		lm.lock.Lock()
		defer lm.lock.Unlock()
		lm.locks[key] = &sync.Mutex{}
		return lm.locks[key]
	}
	lm.lock.RLock()
	defer lm.lock.RUnlock()
	return lm.locks[key]
}

func (lm *LockManager) HasLock(key string) bool {
	lm.lock.RLock()
	defer lm.lock.RUnlock()

	_, ok := lm.locks[key]
	return ok
}

func (lm *LockManager) DelLock(key string) {
	if !lm.HasLock(key) {
		return
	}
	lm.lock.Lock()
	defer lm.lock.Unlock()
	delete(lm.locks, key)
}
