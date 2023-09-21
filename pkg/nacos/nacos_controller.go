package nacos

import (
	"context"
	"fmt"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/pkg/nacos/auth"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

type SyncConfigurationController struct {
	client.Client
	mappings                 *DataId2DCMappings
	locks                    *LockManager
	authManager              *auth.NacosAuthManager
	authProvider             auth.NacosAuthProvider
	server2ClusterCallbackFn func(namespace, group, dataId, content string)
}

type SyncConfigOptions struct {
	AuthManger   *auth.NacosAuthManager
	AuthProvider auth.NacosAuthProvider
	Callback     Server2ClusterCallback
	Mappings     *DataId2DCMappings
	Locks        *LockManager
}

func NewSyncConfigurationController(c client.Client, opt SyncConfigOptions) *SyncConfigurationController {
	if opt.AuthProvider == nil {
		opt.AuthProvider = &auth.DefaultNaocsAuthProvider{Client: c}
	}
	if opt.AuthManger == nil {
		opt.AuthManger = auth.GetNacosAuthManger()
	}
	if opt.Mappings == nil {
		opt.Mappings = NewDataId2DCMappings()
	}
	if opt.Locks == nil {
		opt.Locks = NewLockManager()
	}
	if opt.Callback == nil {
		opt.Callback = NewDefaultServer2ClusterCallback(c, opt.Mappings, opt.Locks)
	}
	return &SyncConfigurationController{
		Client:                   c,
		mappings:                 opt.Mappings,
		locks:                    opt.Locks,
		authManager:              opt.AuthManger,
		authProvider:             opt.AuthProvider,
		server2ClusterCallbackFn: opt.Callback.Callback,
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
	configClient, err := scc.authManager.GetNacosConfigClient(scc.authProvider, dc)
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
	configClient, err := scc.authManager.GetNacosConfigClient(scc.authProvider, dc)
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
	l = l.WithValues("group", group, "namespace", dc.Spec.NacosServer.Namespace)
	var errDataIdList []string
	for _, dataId := range dc.Spec.DataIds {
		logWithId := l.WithValues("dataId", dataId)
		content, exist, err := objWrapper.GetContent(dataId)
		if err != nil {
			logWithId.Error(err, "read content from object reference error", "objRef", objRef.String())
			errDataIdList = append(errDataIdList, dataId)
			continue
		}
		contentMd5 := CalcMd5(content)
		// compare content md5 if it is changed
		lastSyncStatus := GetSyncStatusByDataId(dc.Status.SyncStatuses, dataId)
		if lastSyncStatus != nil && lastSyncStatus.Ready {
			if contentMd5 == lastSyncStatus.Md5 {
				logWithId.Info("skip syncing, due to same md5 of content", "md5", contentMd5)
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
				logWithId.Info("dataId deleted in nacos server")
				if err != nil {
					logWithId.Error(err, "delete dataId error")
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
				logWithId.Error(err, "get dataId error")
				errDataIdList = append(errDataIdList, dataId)
				UpdateSyncStatus(dc, dataId, contentMd5, syncFrom, metav1.Now(), false, err.Error())
				continue
			}
			if len(conf) > 0 {
				logWithId.Info("skip syncing, due to SyncPolicy IfAbsent and server has config already.")
				if lastSyncStatus != nil {
					UpdateSyncStatus(dc, dataId, lastSyncStatus.Md5, lastSyncStatus.LastSyncFrom, lastSyncStatus.LastSyncTime, true, "skipped, due to SyncPolicy IfAbsent")
				} else {
					UpdateSyncStatus(dc, dataId, CalcMd5(conf), "cluster", metav1.Now(), true, "skipped, due to SyncPolicy IfAbsent")
				}
				continue
			}
		}
		_, err = configClient.PublishConfig(vo.ConfigParam{
			DataId:  dataId,
			Group:   group,
			Content: content,
		})
		if err != nil {
			logWithId.Error(err, "publish config error")
			errDataIdList = append(errDataIdList, dataId)
			UpdateSyncStatus(dc, dataId, contentMd5, syncFrom, metav1.Now(), false, err.Error())
			continue
		}
		logWithId.Info("config published to nacos server")
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
	configClient, err := scc.authManager.GetNacosConfigClient(scc.authProvider, dc)
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
		logWithId := l.WithValues("dataId", dataId)
		content, err := configClient.GetConfig(vo.ConfigParam{
			Group:  group,
			DataId: dataId,
		})
		if err != nil {
			logWithId.Error(err, "read content from server error")
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
			logWithId.Error(err, "read object reference content error")
			errDataIdList = append(errDataIdList, dataId)
			UpdateSyncStatus(dc, dataId, "", "server", metav1.Now(), false, "read object reference content error: "+err.Error())
			continue
		}
		if exist && syncIfAbsent {
			logWithId.Info("skipped due to sync policy IfAbsent", "dataId", dataId)
			continue
		} else if !exist || CalcMd5(oldContent) != CalcMd5(content) {
			anyContentChanged = true
			if err := objWrapper.StoreContent(dataId, content); err != nil {
				logWithId.Error(err, "store content to object reference error", "content", content, "obj", objectRef)
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
				OnChange: scc.server2ClusterCallbackFn,
			})
			if err != nil {
				logWithId.Error(err, "listen dataId error")
				errDataIdList = append(errDataIdList, dataId)
				continue
			}
			logWithId.Info("start listening from nacos server")
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
