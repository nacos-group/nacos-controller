package nacos

import (
	"context"
	"fmt"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

type SyncConfigurationController struct {
	client.Client
}

func (scc *SyncConfigurationController) SyncDynamicConfiguration(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if dc == nil {
		return fmt.Errorf("empty DynamicConfiguration")
	}
	strategy := dc.Spec.Strategy
	switch strategy.SyncDirection {
	case nacosiov1.Server2Cluster:
		return fmt.Errorf("todo server2cluster")
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
	if dc.Spec.Strategy.SyncDirection == nacosiov1.Server2Cluster {
		return nil
	}
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
	objWrapper, err := NewObjectReferenceWrapper(scc.Client, dc, objRef)
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
	if len(errDataIdList) > 0 {
		return fmt.Errorf("err dataIds: %s", strings.Join(errDataIdList, ","))
	}
	dc.Status.ObjectRef = &objRef
	return nil
}
