package nacos

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/pkg"
	nacosclient "github.com/nacos-group/nacos-controller/pkg/nacos/client"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type DynamicConfigurationUpdateController struct {
	client         client.Client
	cs             *kubernetes.Clientset
	locks          *LockManager
	configClient   nacosclient.NacosConfigClient
	fuzzyListenMap sync.Map
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
		client:         c,
		cs:             cs,
		locks:          opt.Locks,
		configClient:   opt.ConfigClient,
		fuzzyListenMap: sync.Map{},
	}
}

func (scc *DynamicConfigurationUpdateController) SyncDynamicConfiguration(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	fmt.Printf("hahahahah")
	if dc == nil {
		return fmt.Errorf("empty DynamicConfiguration")
	}
	strategy := dc.Spec.Strategy
	mark := strconv.Itoa(rand.Intn(100))
	l := log.FromContext(ctx, "DynamicConfigurationSync", dc.Namespace+"."+dc.Name)
	l.WithValues("mark", mark)
	var err error
	if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopeFull {
		err = scc.syncFull(&l, ctx, dc)
	} else if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopePartial {
		err = scc.syncPartial(&l, ctx, dc)
	} else {
		return fmt.Errorf("unsupport sync scope: %s", string(strategy.SyncScope))
	}
	if err != nil {
		return err
	}
	return scc.FuzzyListenConfig(&l, ctx, dc)
}

type FuzzyListenTask struct {
	dcKey        types.NamespacedName
	stopSignal   chan struct{}
	configClient nacosclient.NacosConfigClient
	client       client.Client
	ctx          context.Context
	cs           *kubernetes.Clientset
	locks        *LockManager
}

func (f *FuzzyListenTask) stop() {
	go func() {
		l := log.FromContext(f.ctx, "FuzzyListenLoop", f.dcKey.Namespace+"."+f.dcKey.Name)
		l.Info("stop signal send")
		f.stopSignal <- struct{}{}
	}()
}
func (f *FuzzyListenTask) start() {
	go func() {
		l := log.FromContext(f.ctx, "FuzzyListenLoop", f.dcKey.Namespace+"."+f.dcKey.Name)
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				l.Info("start fuzzy listen")
				err := f.FuzzyListen()
				if err != nil {
					return
				}
			case <-f.ctx.Done():
				l.Info("stop fuzzy listen")
				return
			case <-f.stopSignal:
				l.Info("stop fuzzy listen")
				return
			}
			timer.Reset(15 * time.Second)
		}
	}()
}

func (f *FuzzyListenTask) FuzzyListen() error {
	l := log.FromContext(f.ctx, "FuzzyListen", f.dcKey.Namespace+"."+f.dcKey.Name)
	dc := &nacosiov1.DynamicConfiguration{}
	if err := f.client.Get(f.ctx, f.dcKey, dc); err != nil {
		l.Error(err, "get DynamicConfiguration error")
		return err
	}
	fuzzyPatternList := make([]FuzzyPattern, 0)
	if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopeFull {
		fuzzyPatternList = append(fuzzyPatternList, FuzzyPattern{
			DataId: "*",
			Group:  "configmap.*",
		})
		fuzzyPatternList = append(fuzzyPatternList, FuzzyPattern{
			DataId: "*",
			Group:  "secret.*",
		})
	} else if dc.Spec.Strategy.SyncScope == nacosiov1.SyncScopePartial {
		for _, objectRef := range dc.Spec.ObjectRefs {
			fuzzyPatternList = append(fuzzyPatternList, FuzzyPattern{
				DataId: "*",
				Group:  strings.ToLower(objectRef.Kind) + "." + objectRef.Name,
			})
		}
	}

	checkConfigList := make([]model.ConfigItem, 0)
	for _, fuzzyPattern := range fuzzyPatternList {
		var pageNo = 1
		var pageSize = 20
		for {
			configPage, err := f.configClient.SearchConfigs(nacosclient.SearchConfigParam{
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
				Group:    fuzzyPattern.Group,
				DataId:   fuzzyPattern.DataId,
				PageNo:   pageNo,
				PageSize: pageSize,
			})
			if err != nil || configPage == nil || configPage.PageItems == nil {
				break
			}
			checkConfigList = append(checkConfigList, configPage.PageItems...)
			if len(configPage.PageItems) == pageSize {
				pageNo++
			} else {
				break
			}
		}
	}
	for _, checkConfig := range checkConfigList {
		if dc.Status.ListenConfigs == nil || dc.Status.ListenConfigs[checkConfig.Group] == nil || !pkg.Contains(dc.Status.ListenConfigs[checkConfig.Group], checkConfig.DataId) {
			l.Info("fuzzy listen config", "group", checkConfig.Group, "dataId", checkConfig.DataId)
			kind := strings.Split(checkConfig.Group, ".")[0]
			if kind == "configmap" {
				kind = "ConfigMap"
			} else if kind == "secret" {
				kind = "Secret"
			}
			objectRef := v1.ObjectReference{
				Kind:       kind,
				APIVersion: "v1",
				Namespace:  dc.Namespace,
				Name:       strings.Split(checkConfig.Group, ".")[1],
			}
			objWrapper, err := NewObjectReferenceWrapper(f.cs, dc, &objectRef)
			if err != nil {
				l.Error(err, "create object reference wrapper error", "obj", objectRef)
				continue
			}
			content, _, _ := objWrapper.GetContentLatest(checkConfig.DataId)
			if len(content) > 0 {
				continue
			}
			err = objWrapper.StoreContentLatest(checkConfig.DataId, checkConfig.Content)
			if err != nil {
				l.Error(err, "store content error", "dc", dc.Name, "group", checkConfig.Group, "dataId", checkConfig.DataId)
				continue
			}
		}
	}
	return nil
}

type FuzzyPattern struct {
	Group  string
	DataId string
}

func (scc *DynamicConfigurationUpdateController) FuzzyListenConfig(l *logr.Logger, ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	cacheKey := fmt.Sprintf("%s-%s", dc.Namespace, dc.Name)
	if task, ok := scc.fuzzyListenMap.Load(cacheKey); ok {
		_, transferOk := task.(FuzzyListenTask)
		if transferOk {
			return nil
		}
	}
	fuzzyListenTask := FuzzyListenTask{
		dcKey: types.NamespacedName{
			Namespace: dc.Namespace,
			Name:      dc.Name,
		},
		stopSignal:   make(chan struct{}),
		configClient: scc.configClient,
		client:       scc.client,
		ctx:          ctx,
		cs:           scc.cs,
		locks:        scc.locks,
	}
	scc.fuzzyListenMap.Store(cacheKey, fuzzyListenTask)
	fuzzyListenTask.start()
	return nil
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
	cacheKey := fmt.Sprintf("%s-%s", dc.Namespace, dc.Name)
	l.Info("try to stop fuzzy listen task")
	if task, ok := scc.fuzzyListenMap.Load(cacheKey); ok {
		if fuzzyListenTask, transferOk := task.(FuzzyListenTask); transferOk {
			fuzzyListenTask.stop()
		}
		scc.fuzzyListenMap.Delete(cacheKey)
	} else {
		l.Info("fuzzy listen task not found")
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
