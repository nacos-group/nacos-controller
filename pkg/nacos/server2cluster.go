package nacos

import (
	"context"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	v1 "k8s.io/api/core/v1"
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

func NewDefaultServer2ClusterCallback(c client.Client, cs *kubernetes.Clientset, locks *LockManager, dcKey types.NamespacedName, objectRef v1.ObjectReference) Server2ClusterCallback {
	return &DefaultServer2ClusterCallback{
		Client:    c,
		cs:        cs,
		locks:     locks,
		dcKey:     dcKey,
		objectRef: objectRef,
	}
}

type DefaultServer2ClusterCallback struct {
	Client    client.Client
	cs        *kubernetes.Clientset
	locks     *LockManager
	dcKey     types.NamespacedName
	objectRef v1.ObjectReference
}

func (cb *DefaultServer2ClusterCallback) Callback(namespace, group, dataId, content string) {
	cb.CallbackWithContext(context.Background(), namespace, group, dataId, content)
}

func (cb *DefaultServer2ClusterCallback) CallbackWithContext(ctx context.Context, namespace, group, dataId, content string) {
	l := log.FromContext(ctx, "dynamicConfiguration", cb.dcKey.String(), "namespace", namespace, "group", group, "dataId", dataId, "type", "listenCallBack")
	l.Info("server2cluster callback for " + cb.dcKey.String())
	if err := retry.RetryOnConflict(wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2,
		Steps:    3,
	}, func() error {
		lockName := cb.dcKey.String()
		lock := cb.locks.GetLock(lockName)
		lock.Lock()
		defer lock.Unlock()
		return cb.syncConfigToLocal(ctx, namespace, group, dataId, content)
	}); err != nil {
		l.Error(err, "update config failed", "dc", cb.dcKey)
	}
	l.Info("server2cluster callback processed," + cb.dcKey.String())
	return
}

func (cb *DefaultServer2ClusterCallback) syncConfigToLocal(ctx context.Context, namespace, group, dataId, content string) error {
	l := log.FromContext(ctx)
	l = l.WithValues("dynamicConfiguration", cb.dcKey.String(), "namespace", namespace, "group", group, "dataId", dataId, "type", "listenCallBack")
	dc := nacosiov1.DynamicConfiguration{}
	if err := cb.Client.Get(ctx, cb.dcKey, &dc); err != nil {
		if errors.IsNotFound(err) {
			l.Info("DynamicConfiguration not found")
			return nil
		}
		l.Error(err, "get DynamicConfiguration error")
		return err
	}
	objWrapper, err := NewObjectReferenceWrapper(cb.cs, &dc, &cb.objectRef)
	if err != nil {
		l.Error(err, "create object reference wrapper error", "obj", cb.objectRef)
		return err
	}
	oldContent, _, err := objWrapper.GetContent(dataId)
	if err != nil {
		l.Error(err, "read content error")
		return err
	}
	if len(content) == 0 && dc.Spec.Strategy.SyncDeletion == false {
		l.Info("ignored due to syncDeletion is false", "dc", dc.Name)
		UpdateSyncStatusIfAbsent(&dc, group, dataId, "", "server", metav1.Now(), true, "skipped due to syncDeletion is false")
		return cb.Client.Status().Update(ctx, &dc)
	}
	newMd5 := CalcMd5(content)
	if newMd5 == CalcMd5(oldContent) {
		l.Info("ignored due to same content", "md5", newMd5)
		UpdateSyncStatusIfAbsent(&dc, group, dataId, newMd5, "server", metav1.Now(), true, "skipped due to same md5")
		return nil
	}
	if err := objWrapper.StoreContent(dataId, content); err != nil {
		l.Error(err, "update content error", "obj", cb.objectRef)
		return err
	}
	l.Info("update content success", "newContent", content, "oldContent", oldContent)
	UpdateSyncStatus(&dc, group, dataId, newMd5, "server", metav1.Now(), true, "")
	if err := cb.Client.Status().Update(ctx, &dc); err != nil {
		l.Error(err, "update status error")
		return err
	}
	if err := objWrapper.Flush(); err != nil {
		l.Error(err, "flush object reference error")
		return err
	}
	return nil
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
