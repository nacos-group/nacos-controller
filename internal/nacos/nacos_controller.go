package nacos

import (
	"context"
	"fmt"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
	"time"
)

type SyncConfigurationController struct {
	client.Client
	mappings *DataId2DCMappings
	locks    *LockManager
}

func NewSyncConfigurationController(c client.Client) *SyncConfigurationController {
	return &SyncConfigurationController{
		Client:   c,
		mappings: NewDataId2DCMappings(),
		locks:    NewLockManager(),
	}
}

func (scc *SyncConfigurationController) SyncDynamicConfiguration(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if dc == nil {
		return fmt.Errorf("empty DynamicConfiguration")
	}
	strategy := dc.Spec.Strategy
	switch strategy.SyncDirection {
	case nacosiov1.Server2Cluster:
		return scc.syncServer2Cluster(ctx, dc)
	case nacosiov1.Cluster2Server:
		return scc.syncCluster2Server(ctx, dc)
	default:
		return fmt.Errorf("unsupport sync direction: %s", string(strategy.SyncDirection))
	}
}

func (scc *SyncConfigurationController) Finalize(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if dc == nil {
		return nil
	}
	switch dc.Spec.Strategy.SyncDirection {
	case nacosiov1.Server2Cluster:
		return scc.finalizeServer2Cluster(ctx, dc)
	case nacosiov1.Cluster2Server:
		return scc.finalizeCluster2Server(ctx, dc)
	default:
		return fmt.Errorf("not support sync direction: " + string(dc.Spec.Strategy.SyncDirection))
	}
}

func (scc *SyncConfigurationController) finalizeServer2Cluster(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	nn := types.NamespacedName{Name: dc.Name, Namespace: dc.Namespace}
	scc.locks.DelLock(nn.String())
	namespace := dc.Spec.NacosServer.Namespace
	group := dc.Spec.NacosServer.Group
	for _, dataId := range dc.Spec.DataIds {
		scc.mappings.RemoveMapping(namespace, group, dataId, nn)
	}
	return nil
}

func (scc *SyncConfigurationController) finalizeCluster2Server(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if !dc.Spec.Strategy.SyncDeletion {
		return nil
	}
	l := log.FromContext(ctx)
	configClient, err := GetOrCreateNacosConfigClient(scc.Client, dc)
	if err != nil {
		return err
	}
	group := dc.Spec.NacosServer.Group
	var errDataIdList []string
	for _, dataId := range dc.Spec.DataIds {
		_, err := configClient.DeleteConfig(vo.ConfigParam{
			Group:  group,
			DataId: dataId,
		})
		if err != nil {
			l.Error(err, "delete dataId error", "dataId", dataId)
			errDataIdList = append(errDataIdList, dataId)
			UpdateSyncStatus(dc, dataId, "", "cluster-finalizer", metav1.Now(), false, err.Error())
		}
	}
	if len(errDataIdList) > 0 {
		return fmt.Errorf("err dataIds: %s", strings.Join(errDataIdList, ","))
	}
	return nil
}

// syncCluster2Server read DataIds from dc.spec.dataIds， and then read content from dc.spec.objectRef.
// Compare content from objectRef with nacos server, and update nacos server side depend on dc.spec.strategy.syncPolicy
func (scc *SyncConfigurationController) syncCluster2Server(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	l := log.FromContext(ctx)
	configClient, err := GetOrCreateNacosConfigClient(scc.Client, dc)
	if err != nil {
		l.Error(err, "create nacos config client error")
		return err
	}

	objRef := v1.ObjectReference{
		Namespace:  dc.Namespace,
		Name:       dc.Spec.ObjectRef.Name,
		APIVersion: dc.Spec.ObjectRef.APIVersion,
		Kind:       dc.Spec.ObjectRef.Kind,
	}
	objWrapper, err := NewObjectReferenceWrapper(scc.Client, dc, &objRef)
	if err != nil {
		l.Error(err, "create object wrapper error", "obj", objRef)
		return err
	}
	group := dc.Spec.NacosServer.Group
	syncFrom := "cluster"
	var errDataIdList []string
	for _, dataId := range dc.Spec.DataIds {
		content, exist, err := objWrapper.GetContent(dataId)
		if err != nil {
			l.Error(err, "read content from object reference error", "dataId", dataId, "objRef", objRef.String())
			errDataIdList = append(errDataIdList, dataId)
			continue
		}
		contentMd5 := CalcMd5(content)
		// compare content md5 if it is changed
		lastSyncStatus := GetSyncStatusByDataId(dc.Status.SyncStatuses, dataId)
		if lastSyncStatus != nil && lastSyncStatus.Ready {
			if contentMd5 == lastSyncStatus.Md5 {
				l.Info("skip syncing, due to same md5 of content", "md5", contentMd5, "dataId", dataId)
				continue
			}
		}
		if !exist {
			// If dataId is not exist in cluster, and syncDeletion is true. Then we delete dataId in nacos server.
			if dc.Spec.Strategy.SyncDeletion {
				_, err := configClient.DeleteConfig(vo.ConfigParam{
					Group:  group,
					DataId: dataId,
				})
				l.Info("dataId deleted in nacos server", "dataId", dataId)
				if err != nil {
					l.Error(err, "delete dataId error", "dataId", dataId)
					errDataIdList = append(errDataIdList, dataId)
					UpdateSyncStatus(dc, dataId, contentMd5, syncFrom, metav1.Now(), false, err.Error())
					continue
				}
			}
			UpdateSyncStatus(dc, dataId, "", syncFrom, metav1.Now(), true, "dataId deleted in cluster")
			continue
		}
		// If syncPolicy is IfAbsent, then we check the dataId in nacos server first
		if dc.Spec.Strategy.SyncPolicy == nacosiov1.IfAbsent {
			conf, err := configClient.GetConfig(vo.ConfigParam{
				Group:  group,
				DataId: dataId,
			})
			if err != nil {
				l.Error(err, "get dataId error", "dataId", dataId)
				errDataIdList = append(errDataIdList, dataId)
				UpdateSyncStatus(dc, dataId, contentMd5, syncFrom, metav1.Now(), false, err.Error())
				continue
			}
			if len(conf) > 0 {
				l.Info("skip syncing, due to SyncPolicy IfAbsent and server has config already.", "dataId", dataId)
				UpdateSyncStatus(dc, dataId, lastSyncStatus.Md5, lastSyncStatus.LastSyncFrom, lastSyncStatus.LastSyncTime, true, "skipped, due to SyncPolicy IfAbsent")
				continue
			}
		}
		_, err = configClient.PublishConfig(vo.ConfigParam{
			DataId:  dataId,
			Group:   group,
			Content: content,
		})
		if err != nil {
			l.Error(err, "publish config error", "dataId", dataId)
			errDataIdList = append(errDataIdList, dataId)
			UpdateSyncStatus(dc, dataId, contentMd5, syncFrom, metav1.Now(), false, err.Error())
			continue
		}
		l.Info("published dataId: " + dataId)
		UpdateSyncStatus(dc, dataId, contentMd5, syncFrom, metav1.Now(), true, "")
	}

	var removedDataIds []string
	for _, status := range dc.Status.SyncStatuses {
		if !StringSliceContains(dc.Spec.DataIds, status.DataId) {
			removedDataIds = append(removedDataIds, status.DataId)
		}
	}
	for _, dataId := range removedDataIds {
		RemoveSyncStatus(dc, dataId)
	}
	if len(errDataIdList) > 0 {
		return fmt.Errorf("err dataIds: %s", strings.Join(errDataIdList, ","))
	}
	dc.Status.ObjectRef = &objRef
	return nil
}

func (scc *SyncConfigurationController) syncServer2Cluster(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	l := log.FromContext(ctx)
	configClient, err := GetOrCreateNacosConfigClient(scc.Client, dc)
	if err != nil {
		l.Error(err, "create nacos config client error")
		return err
	}

	var objectRef v1.ObjectReference
	if dc.Spec.ObjectRef == nil {
		// if user doesn't specify objectRef in spec, then generate a configmap with same name
		apiVersion, kind := ConfigMapGVK.ToAPIVersionAndKind()
		objectRef = v1.ObjectReference{
			Namespace:  dc.Namespace,
			Name:       dc.Name,
			Kind:       kind,
			APIVersion: apiVersion,
		}
	} else {
		objectRef = *(objectRef.DeepCopy())
		objectRef.Namespace = dc.Namespace
	}
	dc.Status.ObjectRef = &objectRef

	objWrapper, err := NewObjectReferenceWrapper(scc.Client, dc, &objectRef)
	if err != nil {
		l.Error(err, "create object wrapper error")
		return err
	}

	group := dc.Spec.NacosServer.Group
	namespace := dc.Spec.NacosServer.Namespace
	var errDataIdList []string
	dataMap := map[string]string{}

	l = l.WithValues("group", group, "namespace", namespace)
	anyContentChanged := false
	syncIfAbsent := dc.Spec.Strategy.SyncPolicy == nacosiov1.IfAbsent
	for _, dataId := range dc.Spec.DataIds {
		content, err := configClient.GetConfig(vo.ConfigParam{
			Group:  group,
			DataId: dataId,
		})
		if err != nil {
			l.Error(err, "read content from server error", "dataId", dataId)
			errDataIdList = append(errDataIdList, dataId)
			UpdateSyncStatus(dc, dataId, "", "server", metav1.Now(), false, "read content from server error: "+err.Error())
			continue
		}
		dataMap[dataId] = content
		nn := types.NamespacedName{
			Namespace: dc.Namespace,
			Name:      dc.Name,
		}
		oldContent, exist, err := objWrapper.GetContent(dataId)
		if err != nil {
			l.Error(err, "read object reference content error", "dataId", dataId)
			errDataIdList = append(errDataIdList, dataId)
			UpdateSyncStatus(dc, dataId, "", "server", metav1.Now(), false, "read object reference content error: "+err.Error())
			continue
		}
		if exist && syncIfAbsent {
			l.Info("skipped due to sync policy IfAbsent", "dataId", dataId)
			continue
		} else if !exist || CalcMd5(oldContent) != CalcMd5(content) {
			anyContentChanged = true
			if err := objWrapper.StoreContent(dataId, content); err != nil {
				l.Error(err, "store content to object reference error", "dataId", dataId, "content", content, "obj", objectRef)
				errDataIdList = append(errDataIdList, dataId)
				UpdateSyncStatus(dc, dataId, "", "server", metav1.Now(), false, "store content to object reference error: "+err.Error())
				continue
			}
			UpdateSyncStatus(dc, dataId, CalcMd5(content), "server", metav1.Now(), true, "")
		} else {
			UpdateSyncStatusIfAbsent(dc, dataId, CalcMd5(content), "server", metav1.Now(), true, "skipped due to same md5")
		}
		if syncIfAbsent {
			scc.mappings.RemoveMapping(namespace, group, dataId, nn)
			continue
		}
		if !scc.mappings.HasMapping(namespace, group, dataId, nn) {
			err = configClient.ListenConfig(vo.ConfigParam{
				Group:    group,
				DataId:   dataId,
				OnChange: scc.server2ClusterCallback,
			})
			if err != nil {
				l.Error(err, "listen dataId error", "dataId", dataId)
				errDataIdList = append(errDataIdList, dataId)
				continue
			}
			l.Info("start listening from nacos server", "dataId", dataId, "group", group, "namespace", namespace)
			scc.mappings.AddMapping(namespace, group, dataId, nn)
		}
	}
	if anyContentChanged {
		if err := objWrapper.Flush(); err != nil {
			l.Error(err, "flush object reference error")
			return err
		}
	}

	var removedDataIds []string
	for _, status := range dc.Status.SyncStatuses {
		if !StringSliceContains(dc.Spec.DataIds, status.DataId) {
			removedDataIds = append(removedDataIds, status.DataId)
		}
	}
	for _, dataId := range removedDataIds {
		dcNNList := scc.mappings.GetDCList(namespace, group, dataId)
		if len(dcNNList) == 0 {
			l.Info("no DynamicConfiguration listen to this dataId, stop listening from nacos server", "dataId", dataId)
			if err := configClient.CancelListenConfig(vo.ConfigParam{
				Group:  group,
				DataId: dataId,
			}); err != nil {
				l.Error(err, "cancel listening dataId error", "dataId", dataId)
				UpdateSyncStatus(dc, dataId, "", "controller", metav1.Now(), false, "cancel listening error: "+err.Error())
				errDataIdList = append(errDataIdList, dataId)
				continue
			}
		}
		RemoveSyncStatus(dc, dataId)
	}

	if len(errDataIdList) > 0 {
		return fmt.Errorf("error dataIds: " + strings.Join(errDataIdList, ","))
	}
	return nil
}

func (scc *SyncConfigurationController) server2ClusterCallback(namespace, group, dataId, content string) {
	ctx := context.TODO()
	l := log.FromContext(ctx, "namespace", namespace, "group", group, "dataId", dataId)
	ctx = log.IntoContext(ctx, l)
	l.Info("server2cluster callback")
	dcNNList := scc.mappings.GetDCList(namespace, group, dataId)
	for _, nn := range dcNNList {
		if err := retry.RetryOnConflict(wait.Backoff{
			Duration: 1 * time.Second,
			Factor:   2,
			Steps:    3,
		}, func() error {
			// 以DC纬度，锁住更新
			l.Info("server2cluster callback for " + nn.String())
			lockName := nn.String()
			lock := scc.locks.GetLock(lockName)
			lock.Lock()
			defer lock.Unlock()
			return scc.server2ClusterCallbackOneDC(ctx, namespace, group, dataId, content, nn)
		}); err != nil {
			l.Error(err, "update config failed", "dc", nn)
		}
	}
	l.Info("server2cluster callback processed", "dcList", dcNNList)
}

func (scc *SyncConfigurationController) server2ClusterCallbackOneDC(ctx context.Context, namespace, group, dataId, content string, nn types.NamespacedName) error {
	l := log.FromContext(ctx)
	dc := nacosiov1.DynamicConfiguration{}
	if err := scc.Get(ctx, nn, &dc); err != nil {
		if errors.IsNotFound(err) {
			scc.mappings.RemoveMapping(namespace, group, dataId, nn)
			l.Info("mapping removed due to dc not found", "dc", nn)
			return nil
		}
		l.Error(err, "get DynamicConfiguration error", "dc", nn)
		return err
	}
	if !StringSliceContains(dc.Spec.DataIds, dataId) {
		scc.mappings.RemoveMapping(namespace, group, dataId, nn)
		l.Info("mapping removed due to dataId not found in spec.DataIds", "dc", nn)
		return nil
	}
	if dc.Spec.NacosServer.Namespace != namespace {
		scc.mappings.RemoveMapping(namespace, group, dataId, nn)
		l.Info("mapping removed due to namespace changed", "dc", nn, "namespace from server", namespace, "namespace in dc", dc.Spec.NacosServer.Namespace)
		return nil
	}
	if dc.Spec.NacosServer.Group != group {
		scc.mappings.RemoveMapping(namespace, group, dataId, nn)
		l.Info("mapping removed due to group changed", "dc", nn, "group from server", group, "group in dc", dc.Spec.NacosServer.Group)
		return nil
	}
	if dc.Status.ObjectRef == nil {
		err := fmt.Errorf("ObjectRefence empty in status")
		l.Error(err, "dc", nn)
		return err
	}

	objRef := dc.Status.ObjectRef.DeepCopy()
	objRef.Namespace = dc.Namespace
	objWrapper, err := NewObjectReferenceWrapper(scc.Client, &dc, objRef)
	if err != nil {
		l.Error(err, "create object wrapper error", "objRef", objRef, "dc", nn)
		return err
	}
	oldContent, _, err := objWrapper.GetContent(dataId)
	if err != nil {
		l.Error(err, "read content error")
		return err
	}
	newMd5 := CalcMd5(content)
	if newMd5 == CalcMd5(oldContent) {
		l.Info("ignored due to same content", "dc", nn, "md5", newMd5)
		UpdateSyncStatusIfAbsent(&dc, dataId, newMd5, "server", metav1.Now(), true, "skipped due to same md5")
		return nil
	}
	if err := objWrapper.StoreContent(dataId, content); err != nil {
		l.Error(err, "update content error", "obj", objRef)
		return err
	}
	UpdateSyncStatus(&dc, dataId, newMd5, "server", metav1.Now(), true, "")
	return scc.Status().Update(ctx, &dc)
}
