package nacos

import (
	"fmt"
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/pkg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

func UpdateSyncStatus(dc *nacosiov1.DynamicConfiguration, group, dataId, md5, from string, t metav1.Time, ready bool, message string) {
	if dc == nil {
		return
	}
	if len(dc.Status.SyncStatuses) == 0 {
		dc.Status.SyncStatuses = make(map[string][]nacosiov1.SyncStatus)
	}
	syncStatuses := dc.Status.SyncStatuses
	groupSyncStatuses := syncStatuses[group]
	groupSyncStatuses = replaceSyncStatus(groupSyncStatuses, nacosiov1.SyncStatus{
		DataId:       dataId,
		LastSyncFrom: from,
		LastSyncTime: t,
		Ready:        ready,
		Message:      message,
		Md5:          md5,
	})
	syncStatuses[group] = groupSyncStatuses
	dc.Status.SyncStatuses = syncStatuses
}

func UpdateSyncStatusIfAbsent(dc *nacosiov1.DynamicConfiguration, group, dataId, md5, from string, t metav1.Time, ready bool, message string) {
	if dc == nil {
		return
	}
	if len(dc.Status.SyncStatuses) == 0 {
		dc.Status.SyncStatuses = make(map[string][]nacosiov1.SyncStatus)
	}
	syncStatuses := dc.Status.SyncStatuses
	if GetSyncStatusByDataId(dc, group, dataId) != nil {
		return
	}
	groupSyncStatuses := syncStatuses[group]
	if len(groupSyncStatuses) == 0 {
		groupSyncStatuses = []nacosiov1.SyncStatus{}
	}
	groupSyncStatuses = append(groupSyncStatuses, nacosiov1.SyncStatus{
		DataId:       dataId,
		LastSyncFrom: from,
		LastSyncTime: t,
		Ready:        ready,
		Message:      message,
		Md5:          md5,
	})
	syncStatuses[group] = groupSyncStatuses
	dc.Status.SyncStatuses = syncStatuses
}

func RemoveSyncStatus(dc *nacosiov1.DynamicConfiguration, group, dataId string) {
	if dc == nil || len(dc.Status.SyncStatuses) == 0 {
		return
	}
	if len(dc.Status.SyncStatuses) == 0 {
		dc.Status.SyncStatuses = make(map[string][]nacosiov1.SyncStatus)
	}
	groupSyncStatues := dc.Status.SyncStatuses[group]
	if len(groupSyncStatues) == 0 {
		return
	}
	var newStatus []nacosiov1.SyncStatus
	for _, status := range dc.Status.SyncStatuses[group] {
		if dataId == status.DataId {
			continue
		}
		newStatus = append(newStatus, status)
	}
	dc.Status.SyncStatuses[group] = newStatus
}

func replaceSyncStatus(statuses []nacosiov1.SyncStatus, s nacosiov1.SyncStatus) []nacosiov1.SyncStatus {
	if len(statuses) == 0 {
		return []nacosiov1.SyncStatus{s}
	}
	idx := -1
	for i, v := range statuses {
		if v.DataId == s.DataId {
			idx = i
			break
		}
	}
	if idx >= 0 {
		statuses[idx] = s
	} else {
		statuses = append(statuses, s)
	}
	return statuses
}

func GetSyncStatusByDataId(dc *nacosiov1.DynamicConfiguration, group, dataId string) *nacosiov1.SyncStatus {
	if len(dc.Status.SyncStatuses) == 0 {
		dc.Status.SyncStatuses = make(map[string][]nacosiov1.SyncStatus)
	}
	statuses := dc.Status.SyncStatuses
	if len(statuses) == 0 {
		return nil
	}
	groupSyncStatues := statuses[group]
	if len(groupSyncStatues) == 0 {
		return nil
	}
	for i := range groupSyncStatues {
		v := groupSyncStatues[i]
		if v.DataId == dataId {
			return &v
		}
	}
	return nil
}

func AddListenConfig(dc *nacosiov1.DynamicConfiguration, group, dataId string) {
	if dc == nil {
		return
	}
	if len(dc.Status.ListenConfigs) == 0 {
		dc.Status.ListenConfigs = make(map[string][]string)
	}
	groupListConfigs := dc.Status.ListenConfigs[group]
	if len(groupListConfigs) == 0 {
		groupListConfigs = []string{dataId}
		dc.Status.ListenConfigs[group] = groupListConfigs
		return
	} else {
		for _, v := range groupListConfigs {
			if v == dataId {
				return
			}
		}
		groupListConfigs = append(groupListConfigs, dataId)
		dc.Status.ListenConfigs[group] = groupListConfigs
		return
	}
}

func UpdateDynamicConfigurationStatus(dc *nacosiov1.DynamicConfiguration) {
	if dc == nil {
		return
	}
	dc.Status.SyncStrategyStatus = dc.Spec.Strategy
	dc.Status.NacosServerStatus.Namespace = dc.Spec.NacosServer.Namespace
	dc.Status.NacosServerStatus.ServerAddr = dc.Spec.NacosServer.ServerAddr
	dc.Status.NacosServerStatus.Endpoint = dc.Spec.NacosServer.Endpoint
	return
}

func CleanDynamicConfigurationStatus(dc *nacosiov1.DynamicConfiguration) {
	if dc == nil {
		return
	}
	dc.Status.ListenConfigs = map[string][]string{}
	dc.Status.SyncStatuses = map[string][]nacosiov1.SyncStatus{}
	dc.Status.SyncStrategyStatus = nacosiov1.SyncStrategy{}
	dc.Status.NacosServerStatus = nacosiov1.NacosServerStatus{}
}

func checkNacosServerChange(dc *nacosiov1.DynamicConfiguration) bool {
	if dc.Spec.NacosServer.Namespace != dc.Status.NacosServerStatus.Namespace {
		return true
	}
	if dc.Spec.NacosServer.ServerAddr != dc.Status.NacosServerStatus.ServerAddr {
		return true
	}
	if dc.Spec.NacosServer.Endpoint != dc.Status.NacosServerStatus.Endpoint {
		return true
	}
	return false
}
func FailedStatus(dc *nacosiov1.DynamicConfiguration, message string) {
	if dc == nil {
		return
	}
	dc.Status.Phase = pkg.PhaseFailed
	dc.Status.Message = message
	dc.Status.ObservedGeneration = dc.Generation
}

func UpdateStatus(dc *nacosiov1.DynamicConfiguration) {
	if dc == nil {
		return
	}
	dc.Status.ObservedGeneration = dc.Generation
	dc.Status.Phase = pkg.PhaseSucceed
	dc.Status.Message = ""

	var notReadyDataIds []string
	for group, groupSyncStatuses := range dc.Status.SyncStatuses {
		for _, dataIdSyncStatus := range groupSyncStatuses {
			if dataIdSyncStatus.Ready {
				continue
			}
			if !dataIdSyncStatus.Ready {
				notReadyDataIds = append(notReadyDataIds, group+"#"+dataIdSyncStatus.DataId)
			}
		}
	}
	if len(notReadyDataIds) > 0 {
		dc.Status.Phase = pkg.PhaseFailed
		dc.Status.Message = fmt.Sprintf("not ready dataIds: %s", strings.Join(notReadyDataIds, ","))
	}
}
