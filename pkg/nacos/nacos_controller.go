package nacos

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	nacosclient "github.com/nacos-group/nacos-controller/pkg/nacos/client"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type DynamicConfigurationUpdateController struct {
	client       client.Client
	cs           *kubernetes.Clientset
	locks        *LockManager
	configClient nacosclient.NacosConfigClient
}

type SyncConfigOptions struct {
	ConfigClient nacosclient.NacosConfigClient
	Locks        *LockManager
}

func NewDynamicConfigurationUpdateController(c client.Client, cs *kubernetes.Clientset, opt SyncConfigOptions) *DynamicConfigurationUpdateController {
	if opt.ConfigClient == nil {
		opt.ConfigClient = nacosclient.GetDefaultNacosClient()
	}
	if opt.Locks == nil {
		opt.Locks = NewLockManager()
	}
	return &DynamicConfigurationUpdateController{
		client:       c,
		cs:           cs,
		locks:        opt.Locks,
		configClient: opt.ConfigClient,
	}
}

func (scc *DynamicConfigurationUpdateController) SyncDynamicConfiguration(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if dc == nil {
		return fmt.Errorf("empty DynamicConfiguration")
	}
	strategy := dc.Spec.Strategy
	mark := strconv.Itoa(rand.Intn(100))
	l := log.FromContext(ctx, "DynamicConfigurationSync", dc.Namespace+"."+dc.Name)
	l.WithValues("mark", mark)
	if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopeFull {
		return scc.syncFull(&l, ctx, dc)
	}
	if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopePartial {
		return scc.syncPartial(&l, ctx, dc)
	}
	return fmt.Errorf("unsupport sync scope: %s", string(strategy.SyncScope))
}

func (scc *DynamicConfigurationUpdateController) Finalize(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if dc == nil {
		return nil
	}
	l := log.FromContext(ctx, "DynamicConfigurationFinalize", dc.Namespace+"."+dc.Name)
	if err := scc.CleanOldNacosConfig(&l, dc); err != nil {
		l.Error(err, "clean old nacos config, cancel config listening error")
		return err
	}
	return nil
}

func (scc *DynamicConfigurationUpdateController) syncFull(l *logr.Logger, ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	historySyncScope := dc.Status.SyncStrategyStatus.SyncScope
	if historySyncScope == "" {
		return scc.syncNew(l, ctx, dc)
	}
	if historySyncScope == nacosiov1.SyncScopeFull {
		return scc.syncFull2Full(l, ctx, dc)
	}
	if historySyncScope == nacosiov1.SyncScopePartial {
		return scc.syncPartial2Full(l, ctx, dc)
	}
	return nil
}

func (scc *DynamicConfigurationUpdateController) syncPartial(l *logr.Logger, ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	historySyncScope := dc.Status.SyncStrategyStatus.SyncScope
	if historySyncScope == "" {
		return scc.syncNew(l, ctx, dc)
	}
	if historySyncScope == nacosiov1.SyncScopeFull {
		return scc.syncFull2Partial(l, ctx, dc)
	}
	if historySyncScope == nacosiov1.SyncScopePartial {
		return scc.syncPartial2Partial(l, ctx, dc)
	}
	return nil
}

func (scc *DynamicConfigurationUpdateController) syncNew(l *logr.Logger, ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	var objectRefs []*v1.ObjectReference
	if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopeFull {
		var configMaps v1.ConfigMapList
		if err := scc.client.List(ctx, &configMaps, client.InNamespace(dc.Namespace)); err != nil {
			l.Error(err, "list ConfigMap error")
			return err
		}
		var secrets v1.SecretList
		if err := scc.client.List(ctx, &secrets, client.InNamespace(dc.Namespace)); err != nil {
			l.Error(err, "list Secret error")
			return err
		}
		for _, cm := range configMaps.Items {
			configMapRef := v1.ObjectReference{
				Namespace:  cm.Namespace,
				Name:       cm.Name,
				Kind:       cm.Kind,
				APIVersion: cm.APIVersion,
			}
			objectRefs = append(objectRefs, &configMapRef)
		}
		for _, secret := range secrets.Items {
			secretRef := v1.ObjectReference{
				Namespace:  secret.Namespace,
				Name:       secret.Name,
				Kind:       secret.Kind,
				APIVersion: secret.APIVersion,
			}
			objectRefs = append(objectRefs, &secretRef)
		}
	} else if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopePartial {
		for _, ref := range dc.Spec.ObjectRefs {
			dcObjectRef := ref.DeepCopy()
			dcObjectRef.Namespace = dc.Namespace
			objectRefs = append(objectRefs, dcObjectRef)
		}
	}
	var errConfigList []string
	for _, objectRef := range objectRefs {
		scc.configGroupSync(l, objectRef, dc, &errConfigList)
	}
	UpdateDynamicConfigurationStatus(dc)
	if len(errConfigList) > 0 {
		return fmt.Errorf("sync Config error: %s", strings.Join(errConfigList, ","))
	}
	return nil
}

func (scc *DynamicConfigurationUpdateController) syncFull2Full(l *logr.Logger, ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if !checkNacosServerChange(dc) {
		l.Info("nacos server not change, skip sync")
		return nil
	}
	if err := scc.CleanOldNacosConfig(l, dc); err != nil {
		l.Error(err, "clean old nacos config, cancel config listening error")
		return err
	}
	CleanDynamicConfigurationStatus(dc)
	return scc.syncNew(l, ctx, dc)
}

func (scc *DynamicConfigurationUpdateController) syncPartial2Full(l *logr.Logger, ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if checkNacosServerChange(dc) {
		if err := scc.CleanOldNacosConfig(l, dc); err != nil {
			l.Error(err, "clean old nacos config, cancel config listening error")
			return err
		}
		CleanDynamicConfigurationStatus(dc)
		return scc.syncNew(l, ctx, dc)
	}
	nacosServerParam := nacosclient.NacosServerParam{
		Endpoint:   dc.Status.NacosServerStatus.Endpoint,
		ServerAddr: dc.Status.NacosServerStatus.ServerAddr,
		Namespace:  dc.Status.NacosServerStatus.Namespace,
	}
	dcKey := types.NamespacedName{
		Namespace: dc.Namespace,
		Name:      dc.Name,
	}
	var objectRefListenMap = make(map[string]*v1.ObjectReference)
	var configMaps v1.ConfigMapList
	if err := scc.client.List(ctx, &configMaps, client.InNamespace(dc.Namespace)); err != nil {
		l.Error(err, "list ConfigMap error")
		return err
	}
	var secrets v1.SecretList
	if err := scc.client.List(ctx, &secrets, client.InNamespace(dc.Namespace)); err != nil {
		l.Error(err, "list Secret error")
		return err
	}
	for _, cm := range configMaps.Items {
		configMapRef := v1.ObjectReference{
			Namespace:  cm.Namespace,
			Name:       cm.Name,
			Kind:       cm.Kind,
			APIVersion: cm.APIVersion,
		}
		group := strings.ToLower(configMapRef.Kind) + "." + configMapRef.Name
		objectRefListenMap[group] = &configMapRef
	}
	for _, secret := range secrets.Items {
		secretRef := v1.ObjectReference{
			Namespace:  secret.Namespace,
			Name:       secret.Name,
			Kind:       secret.Kind,
			APIVersion: secret.APIVersion,
		}
		group := strings.ToLower(secretRef.Kind) + "." + secretRef.Name
		objectRefListenMap[group] = &secretRef
	}
	for group := range dc.Status.ListenConfigs {
		_, ok := objectRefListenMap[group]
		if !ok {
			dataIds, _ := dc.Status.ListenConfigs[group]
			for _, dataId := range dataIds {
				err := scc.configClient.CancelListenConfig(nacosclient.NacosConfigParam{
					Key:              dcKey,
					NacosServerParam: nacosServerParam,
					Group:            group,
					DataId:           dataId,
				})
				if err != nil {
					l.Error(err, "cancel listen Config from nacos server error", "NacosServerParam",
						nacosServerParam, "group", group, "dataId", dataId)
				}
			}
			delete(dc.Status.SyncStatuses, group)
			delete(dc.Status.ListenConfigs, group)
		}
	}

	var errConfigList []string
	for group := range objectRefListenMap {
		_, ok := dc.Status.ListenConfigs[group]
		if ok {
			continue
		}
		objectRef := objectRefListenMap[group]
		scc.configGroupSync(l, objectRef, dc, &errConfigList)
	}
	UpdateDynamicConfigurationStatus(dc)
	if len(errConfigList) > 0 {
		return fmt.Errorf("sync Config error: %s", strings.Join(errConfigList, ","))
	}
	return nil
}

func (scc *DynamicConfigurationUpdateController) syncPartial2Partial(l *logr.Logger, ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if checkNacosServerChange(dc) {
		if err := scc.CleanOldNacosConfig(l, dc); err != nil {
			l.Error(err, "clean old nacos config, cancel config listening error")
			return err
		}
		CleanDynamicConfigurationStatus(dc)
		return scc.syncNew(l, ctx, dc)
	}

	nacosServerParam := nacosclient.NacosServerParam{
		Endpoint:   dc.Status.NacosServerStatus.Endpoint,
		ServerAddr: dc.Status.NacosServerStatus.ServerAddr,
		Namespace:  dc.Status.NacosServerStatus.Namespace,
	}
	dcKey := types.NamespacedName{
		Namespace: dc.Namespace,
		Name:      dc.Name,
	}
	var objectRefListenMap = make(map[string]*v1.ObjectReference)
	for _, ref := range dc.Spec.ObjectRefs {
		objectRef := ref.DeepCopy()
		objectRef.Namespace = dc.Namespace
		group := strings.ToLower(objectRef.Kind) + "." + objectRef.Name
		objectRefListenMap[group] = objectRef
	}
	for group := range dc.Status.ListenConfigs {
		_, ok := objectRefListenMap[group]
		if !ok {
			dataIds, _ := dc.Status.ListenConfigs[group]
			for _, dataId := range dataIds {
				err := scc.configClient.CancelListenConfig(nacosclient.NacosConfigParam{
					Key:              dcKey,
					NacosServerParam: nacosServerParam,
					Group:            group,
					DataId:           dataId,
				})
				if err != nil {
					l.Error(err, "cancel listen Config from nacos server error", "NacosServerParam",
						nacosServerParam, "group", group, "dataId", dataId)
				}
			}
			delete(dc.Status.SyncStatuses, group)
			delete(dc.Status.ListenConfigs, group)
		}
	}

	var errConfigList []string
	for group := range objectRefListenMap {
		_, ok := dc.Status.ListenConfigs[group]
		if ok {
			continue
		}
		objectRef := objectRefListenMap[group]
		scc.configGroupSync(l, objectRef, dc, &errConfigList)
	}
	UpdateDynamicConfigurationStatus(dc)
	if len(errConfigList) > 0 {
		return fmt.Errorf("sync Config error: %s", strings.Join(errConfigList, ","))
	}
	return nil
}

func (scc *DynamicConfigurationUpdateController) syncFull2Partial(l *logr.Logger, ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	return scc.syncPartial2Partial(l, ctx, dc)
}

func (scc *DynamicConfigurationUpdateController) configGroupSync(log *logr.Logger, objectRef *v1.ObjectReference, dc *nacosiov1.DynamicConfiguration, errConfigList *[]string) {
	group := strings.ToLower(objectRef.Kind) + "." + objectRef.Name
	logWithGroup := log.WithValues("group", group)
	objWrapper, err := NewObjectReferenceWrapper(scc.cs, dc, objectRef)
	if err != nil {
		logWithGroup.Error(err, "new object reference wrapper error", "objectRef", objectRef)
		return
	}
	dataIds, err := objWrapper.GetAllKeys()
	if err != nil {
		logWithGroup.Error(err, "get object reference keys error", "objectRef", objectRef)
		return
	}
	server2Cluster := NewDefaultServer2ClusterCallback(scc.client, scc.cs, scc.locks, types.NamespacedName{
		Namespace: dc.Namespace,
		Name:      dc.Name,
	}, *objectRef)
	for _, dataId := range dataIds {
		logWithDataId := logWithGroup.WithValues("dataId", dataId)
		localContent, localExist, err := objWrapper.GetContent(dataId)
		if err != nil {
			logWithDataId.Error(err, "get local content error")
			*errConfigList = append(*errConfigList, group+"#"+dataId)
			UpdateSyncStatus(dc, group, dataId, "", "cluster", metav1.Now(), false, "get local content error: "+err.Error())
			continue
		}
		localExist = localExist && len(localContent) > 0
		logWithDataId.Info("try get Config from nacos server")
		serverContent, err := scc.configClient.GetConfig(nacosclient.NacosConfigParam{
			AuthRef: dc.Spec.NacosServer.AuthRef,
			Key: types.NamespacedName{
				Namespace: dc.Namespace,
				Name:      dc.Name,
			},
			NacosServerParam: nacosclient.NacosServerParam{
				Endpoint:   dc.Spec.NacosServer.Endpoint,
				ServerAddr: dc.Spec.NacosServer.ServerAddr,
				Namespace:  dc.Spec.NacosServer.Namespace,
			},
			Group:  group,
			DataId: dataId,
		})
		if err != nil {
			logWithDataId.Info("get Config from nacos server fail, maybe configEmpty")
		} else {
			logWithDataId.Info("get Config from nacos server success")
		}
		serverExist := len(serverContent) > 0
		isPreferCluster := dc.Spec.Strategy.ConflictPolicy == nacosiov1.PreferCluster
		if (serverExist && !localExist) || (serverExist && localExist && !isPreferCluster) {
			serverMd5 := CalcMd5(serverContent)
			logWithDataId.Info("Config not exist in local or preferServer, try to sync Config from nacos server")
			if err := objWrapper.StoreContent(dataId, serverContent); err != nil {
				logWithDataId.Error(err, "store Config to local error")
				*errConfigList = append(*errConfigList, group+"#"+dataId)
				UpdateSyncStatus(dc, group, dataId, "", "server", metav1.Now(), false, "store Config to local error: "+err.Error())
				continue
			} else {
				UpdateSyncStatus(dc, group, dataId, serverMd5, "server", metav1.Now(), true, "")
			}
		} else if (!serverExist && localExist) || (serverExist && localExist && isPreferCluster) {
			localMd5 := CalcMd5(localContent)
			logWithDataId.Info("Config not exist in nacos server or preferCluster, try to sync Config to nacos server")
			if success, err := scc.configClient.PublishConfig(nacosclient.NacosConfigParam{
				AuthRef: dc.Spec.NacosServer.AuthRef,
				Key: types.NamespacedName{
					Namespace: dc.Namespace,
					Name:      dc.Name,
				},
				NacosServerParam: nacosclient.NacosServerParam{
					Endpoint:   dc.Spec.NacosServer.Endpoint,
					ServerAddr: dc.Spec.NacosServer.ServerAddr,
					Namespace:  dc.Spec.NacosServer.Namespace,
				},
				Group:   group,
				DataId:  dataId,
				Content: localContent,
			}); err != nil || !success {
				logWithDataId.Error(err, "publish Config to nacos server error")
				*errConfigList = append(*errConfigList, group+"#"+dataId)
				UpdateSyncStatus(dc, group, dataId, "", "server", metav1.Now(), false, "publish Config to nacos server error: "+err.Error())
				continue
			} else {
				UpdateSyncStatus(dc, group, dataId, localMd5, "config", metav1.Now(), true, "")
			}
		}
		err = scc.configClient.ListenConfig(nacosclient.NacosConfigParam{
			AuthRef: dc.Spec.NacosServer.AuthRef,
			Key: types.NamespacedName{
				Namespace: dc.Namespace,
				Name:      dc.Name,
			},
			NacosServerParam: nacosclient.NacosServerParam{
				Endpoint:   dc.Spec.NacosServer.Endpoint,
				ServerAddr: dc.Spec.NacosServer.ServerAddr,
				Namespace:  dc.Spec.NacosServer.Namespace,
			},
			Group:    group,
			DataId:   dataId,
			OnChange: server2Cluster.Callback,
		})
		if err != nil {
			logWithDataId.Error(err, "listen Config from nacos server error")
			*errConfigList = append(*errConfigList, group+"#"+dataId)
			UpdateSyncStatus(dc, group, dataId, "", "server", metav1.Now(), false, "listen Config from nacos server error: "+err.Error())
			continue
		}
		AddListenConfig(dc, group, dataId)
	}
	if err := objWrapper.Flush(); err != nil {
		logWithGroup.Error(err, "flush object reference wrapper error")
		return
	}
	return
}

func (scc *DynamicConfigurationUpdateController) CleanOldNacosConfig(l *logr.Logger, dc *nacosiov1.DynamicConfiguration) error {
	nacosServerParam := nacosclient.NacosServerParam{
		Endpoint:   dc.Status.NacosServerStatus.Endpoint,
		ServerAddr: dc.Status.NacosServerStatus.ServerAddr,
		Namespace:  dc.Status.NacosServerStatus.Namespace,
	}
	dcKey := types.NamespacedName{
		Namespace: dc.Namespace,
		Name:      dc.Name,
	}
	for group, dataIds := range dc.Status.ListenConfigs {
		for _, dataId := range dataIds {
			err := scc.configClient.CancelListenConfig(nacosclient.NacosConfigParam{
				Key:              dcKey,
				NacosServerParam: nacosServerParam,
				Group:            group,
				DataId:           dataId,
			})
			if err != nil {
				l.Error(err, "cancel listen Config from nacos server error", "NacosServerParam",
					nacosServerParam, "group", group, "dataId", dataId)
				return err
			}
		}
	}
	scc.configClient.CloseClient(nacosclient.NacosConfigParam{
		Key:              dcKey,
		NacosServerParam: nacosServerParam,
	})
	return nil
}
