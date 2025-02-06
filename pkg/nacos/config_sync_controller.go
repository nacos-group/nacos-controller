package nacos

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/pkg"
	nacosclient "github.com/nacos-group/nacos-controller/pkg/nacos/client"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

type ConfigurationSyncController struct {
	client       client.Client
	clientSet    *kubernetes.Clientset
	locks        *LockManager
	configClient nacosclient.NacosConfigClient
}

func NewConfigurationSyncController(client client.Client, cs *kubernetes.Clientset, opt SyncConfigOptions) *ConfigurationSyncController {
	if opt.ConfigClient == nil {
		opt.ConfigClient = nacosclient.GetDefaultNacosClient()
	}
	if opt.Locks == nil {
		opt.Locks = NewLockManager()
	}
	return &ConfigurationSyncController{
		client:       client,
		clientSet:    cs,
		locks:        opt.Locks,
		configClient: opt.ConfigClient,
	}
}

func (r *ConfigurationSyncController) Finalize(ctx context.Context, object client.Object) error {
	l := log.FromContext(ctx)
	l.WithValues("Finalize", object)
	var dynamicConfigurations nacosiov1.DynamicConfigurationList
	if err := r.client.List(ctx, &dynamicConfigurations, client.InNamespace(object.GetNamespace())); err != nil {
		l.Error(err, "list DynamicConfiguration error")
		return err
	}
	nacosGroup := strings.ToLower(object.GetObjectKind().GroupVersionKind().Kind) + "." + object.GetName()
	logWithGroup := l.WithValues("group", nacosGroup)
	localDataIds := GetAllKeys(object)
	for _, dc := range dynamicConfigurations.Items {
		var errConfigList []string
		if !DynamicConfigurationMatch(object, &dc) {
			continue
		}
		listenDataIds, groupExist := dc.Status.ListenConfigs[nacosGroup]
		if !groupExist {
			continue
		}
		_, _, bothIds := CompareDataIds(listenDataIds, localDataIds)
		for _, dataId := range bothIds {
			if dc.Spec.Strategy.SyncDeletion {
				if success, err := r.configClient.DeleteConfig(nacosclient.NacosConfigParam{
					AuthRef: dc.Spec.NacosServer.AuthRef,
					NacosServerParam: nacosclient.NacosServerParam{
						Endpoint:   dc.Spec.NacosServer.Endpoint,
						Namespace:  dc.Spec.NacosServer.Namespace,
						ServerAddr: dc.Spec.NacosServer.ServerAddr,
					},
					Key: types.NamespacedName{
						Namespace: dc.Namespace,
						Name:      dc.Name,
					},
					DataId: dataId,
					Group:  nacosGroup,
				}); err != nil || !success {
					UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), false, fmt.Sprintf("delete config error: %s", err))
					errConfigList = append(errConfigList, nacosGroup+"#"+dataId)
					logWithGroup.Error(err, "delete nacos server config error", "dataId", dataId, "group", nacosGroup)
				} else {
					UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), true, "delete nacos server config success")
					logWithGroup.Info("delete nacos server config success", "dataId", dataId, "group", nacosGroup)
				}
			} else {
				UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), true, "ignore delete nacos server config due to the syncDeletion false")
				logWithGroup.Info("ignore delete nacos server config due to the syncDeletion false", "dataId", dataId, "group", nacosGroup)
			}
		}
		if len(errConfigList) > 0 {
			err := fmt.Errorf("sync Config error: %s", strings.Join(errConfigList, ","))
			logWithGroup.Error(err, "sync ConfigMap config from cluster to server error")
			FailedStatus(&dc, err.Error())
		} else {
			UpdateStatus(&dc)
		}
		err := r.client.Status().Update(ctx, &dc)
		if err != nil {
			logWithGroup.Error(err, "update DynamicConfiguration status error", "DynamicConfiguration", dc)
		}
	}
	return nil
}
func (r *ConfigurationSyncController) DoReconcile(ctx context.Context, object client.Object) error {
	l := log.FromContext(ctx)
	l.WithValues("configMapReconcile", object.GetNamespace()+"."+object.GetName())
	l.Info("doReconcile", "configMap", object.GetNamespace()+"."+object.GetName())
	var dynamicConfigurations nacosiov1.DynamicConfigurationList
	if err := r.client.List(ctx, &dynamicConfigurations, client.InNamespace(object.GetNamespace())); err != nil {
		l.Error(err, "list DynamicConfiguration error")
		return err
	}
	apiVersion, kind := object.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
	nacosGroup := strings.ToLower(kind) + "." + object.GetName()
	logWithGroup := l.WithValues("group", nacosGroup)
	localDataIds := GetAllKeys(object)
	configMapRef := v1.ObjectReference{
		Kind:       kind,
		APIVersion: apiVersion,
		Namespace:  object.GetNamespace(),
		Name:       object.GetName(),
	}
	for _, dc := range dynamicConfigurations.Items {
		var errConfigList []string
		if !DynamicConfigurationMatch(object, &dc) {
			continue
		}
		listenDataIds, groupExist := dc.Status.ListenConfigs[nacosGroup]
		if !groupExist {
			r.configGroupSync(&l, object, &dc, &errConfigList)
			err := r.client.Status().Update(ctx, &dc)
			if err != nil {
				logWithGroup.Error(err, "update DynamicConfiguration status error", "DynamicConfiguration", dc)
			}
			continue
		}
		listenOnly, localOnly, bothIds := CompareDataIds(listenDataIds, localDataIds)
		for _, dataId := range listenOnly {
			if dc.Spec.Strategy.SyncDeletion {
				if success, err := r.configClient.DeleteConfig(nacosclient.NacosConfigParam{
					AuthRef: dc.Spec.NacosServer.AuthRef,
					NacosServerParam: nacosclient.NacosServerParam{
						Endpoint:   dc.Spec.NacosServer.Endpoint,
						Namespace:  dc.Spec.NacosServer.Namespace,
						ServerAddr: dc.Spec.NacosServer.ServerAddr,
					},
					Key: types.NamespacedName{
						Namespace: dc.Namespace,
						Name:      dc.Name,
					},
					DataId: dataId,
					Group:  nacosGroup,
				}); err != nil || !success {
					UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), false, fmt.Sprintf("delete config error: %s", err))
					errConfigList = append(errConfigList, nacosGroup+"#"+dataId)
					logWithGroup.Error(err, "delete nacos server config error", "dataId", dataId, "group", nacosGroup)
				} else {
					UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), true, "delete nacos server config success")
					logWithGroup.Info("delete nacos server config success", "dataId", dataId, "group", nacosGroup)
				}
			} else {
				UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), true, "ignore delete nacos server config due to the syncDeletion false")
				logWithGroup.Info("ignore delete nacos server config due to the syncDeletion false", "dataId", dataId, "group", nacosGroup)
			}
		}
		server2Cluster := NewDefaultServer2ClusterCallback(r.client, r.clientSet, r.locks, types.NamespacedName{
			Namespace: dc.Namespace,
			Name:      dc.Name,
		}, configMapRef)
		for _, dataId := range localOnly {
			r.configDataIdSync(&logWithGroup, dataId, object, &dc, &errConfigList, server2Cluster)
		}
		for _, dataId := range bothIds {
			localContent, localExist := GetContent(object, dataId)
			syncStatus := GetSyncStatusByDataId(&dc, nacosGroup, dataId)
			oldMd5 := syncStatus.Md5
			newMd5 := CalcMd5(localContent)
			if !localExist || len(localContent) == 0 {
				if oldMd5 != newMd5 && dc.Spec.Strategy.SyncDeletion {
					if success, err := r.configClient.DeleteConfig(nacosclient.NacosConfigParam{
						AuthRef: dc.Spec.NacosServer.AuthRef,
						NacosServerParam: nacosclient.NacosServerParam{
							Endpoint:   dc.Spec.NacosServer.Endpoint,
							Namespace:  dc.Spec.NacosServer.Namespace,
							ServerAddr: dc.Spec.NacosServer.ServerAddr,
						},
						Key: types.NamespacedName{
							Namespace: dc.Namespace,
							Name:      dc.Name,
						},
						DataId: dataId,
						Group:  nacosGroup,
					}); err != nil || !success {
						UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), false, fmt.Sprintf("delete config error: %s", err))
						errConfigList = append(errConfigList, nacosGroup+"#"+dataId)
						logWithGroup.Error(err, "delete nacos server config error", "dataId", dataId, "group", nacosGroup)
					} else {
						UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), true, "delete nacos server config success")
						logWithGroup.Info("delete nacos server config success", "dataId", dataId, "group", nacosGroup)
					}
				}
			} else if oldMd5 != newMd5 {
				if success, err := r.configClient.PublishConfig(nacosclient.NacosConfigParam{
					AuthRef: dc.Spec.NacosServer.AuthRef,
					NacosServerParam: nacosclient.NacosServerParam{
						Endpoint:   dc.Spec.NacosServer.Endpoint,
						Namespace:  dc.Spec.NacosServer.Namespace,
						ServerAddr: dc.Spec.NacosServer.ServerAddr,
					},
					Key: types.NamespacedName{
						Namespace: dc.Namespace,
						Name:      dc.Name,
					},
					DataId:  dataId,
					Group:   nacosGroup,
					Content: localContent,
				}); err != nil || !success {
					UpdateSyncStatus(&dc, nacosGroup, dataId, "", "cluster", metav1.Now(), false, fmt.Sprintf("publish config error: %s", err))
					errConfigList = append(errConfigList, nacosGroup+"#"+dataId)
					logWithGroup.Error(err, "publish nacos server config error", "dataId", dataId, "group", nacosGroup)
				} else {
					UpdateSyncStatus(&dc, nacosGroup, dataId, newMd5, "cluster", metav1.Now(), true, "publish nacos server config success")
					logWithGroup.Info("publish nacos server config success", "dataId", dataId, "group", nacosGroup)
				}
			}
		}
		if len(errConfigList) > 0 {
			err := fmt.Errorf("sync Config error: %s", strings.Join(errConfigList, ","))
			logWithGroup.Error(err, "sync ConfigMap config from cluster to server error")
			FailedStatus(&dc, err.Error())
		} else {
			UpdateStatus(&dc)
		}
		err := r.client.Status().Update(ctx, &dc)
		if err != nil {
			logWithGroup.Error(err, "update DynamicConfiguration status error", "DynamicConfiguration", dc)
		}
	}
	return nil
}

func (r *ConfigurationSyncController) ensureFinalizer(ctx context.Context, object client.Object) error {
	if controllerutil.ContainsFinalizer(object, pkg.FinalizerName) {
		return nil
	}
	l := log.FromContext(ctx)
	controllerutil.AddFinalizer(object, pkg.FinalizerName)
	if err := r.client.Update(ctx, object); err != nil {
		l.Error(err, "add ConfigMap finalizer error")
		return err
	}
	return nil
}

func (r *ConfigurationSyncController) configGroupSync(l *logr.Logger, object client.Object, dc *nacosiov1.DynamicConfiguration, errConfigList *[]string) {
	apiVersion, kind := object.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
	group := strings.ToLower(kind) + "." + object.GetName()
	logWithGroup := l.WithValues("group", group)
	dataIds := GetAllKeys(object)
	configMapRef := v1.ObjectReference{
		Kind:       kind,
		APIVersion: apiVersion,
		Namespace:  object.GetNamespace(),
		Name:       object.GetName(),
	}
	server2Cluster := NewDefaultServer2ClusterCallback(r.client, r.clientSet, r.locks, types.NamespacedName{
		Namespace: dc.Namespace,
		Name:      dc.Name,
	}, configMapRef)
	for _, dataId := range dataIds {
		r.configDataIdSync(&logWithGroup, dataId, object, dc, errConfigList, server2Cluster)
	}
	return
}

func (r *ConfigurationSyncController) configDataIdSync(l *logr.Logger, dataId string, object client.Object, dc *nacosiov1.DynamicConfiguration, errConfigList *[]string, server2Cluster Server2ClusterCallback) {
	group := strings.ToLower(object.GetObjectKind().GroupVersionKind().Kind) + "." + object.GetName()
	logWithDataId := l.WithValues("dataId", dataId)
	localContent, localExist := GetContent(object, dataId)
	localExist = localExist && len(localContent) > 0
	logWithDataId.Info("try get Config from nacos server")
	serverContent, err := r.configClient.GetConfig(nacosclient.NacosConfigParam{
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
		logWithDataId.Info("Config not exist in local or preferServer, try to sync Config from nacos server")
		StoreContent(object, dataId, serverContent)
		UpdateSyncStatus(dc, group, dataId, "", "server", metav1.Now(), true, "")

	} else if (!serverExist && localExist) || (serverExist && localExist && isPreferCluster) {
		logWithDataId.Info("Config not exist in nacos server or preferCluster, try to sync Config to nacos server")
		if success, err := r.configClient.PublishConfig(nacosclient.NacosConfigParam{
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
			return
		} else {
			UpdateSyncStatus(dc, group, dataId, "", "config", metav1.Now(), true, "")
		}
	}
	err = r.configClient.ListenConfig(nacosclient.NacosConfigParam{
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
		return
	} else {
		logWithDataId.Info("listen Config from nacos server success", "group", group, "dataId", dataId)
	}
	AddListenConfig(dc, group, dataId)
	return
}
