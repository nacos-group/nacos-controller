package nacos

import (
	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func UpdateSyncStatus(dc *nacosiov1.DynamicConfiguration, dataId, md5, from string, t metav1.Time, ready bool, message string) {
	if dc == nil {
		return
	}
	syncStatuses := dc.Status.SyncStatuses
	syncStatuses = replaceSyncStatus(syncStatuses, nacosiov1.SyncStatus{
		DataId:       dataId,
		LastSyncFrom: from,
		LastSyncTime: t,
		Ready:        ready,
		Message:      message,
		Md5:          md5,
	})
	dc.Status.SyncStatuses = syncStatuses
}

func UpdateSyncStatusIfAbsent(dc *nacosiov1.DynamicConfiguration, dataId, md5, from string, t metav1.Time, ready bool, message string) {
	if dc == nil {
		return
	}
	syncStatuses := dc.Status.SyncStatuses
	if GetSyncStatusByDataId(syncStatuses, dataId) != nil {
		return
	}
	syncStatuses = append(syncStatuses, nacosiov1.SyncStatus{
		DataId:       dataId,
		LastSyncFrom: from,
		LastSyncTime: t,
		Ready:        ready,
		Message:      message,
		Md5:          md5,
	})
	dc.Status.SyncStatuses = syncStatuses
}

func RemoveSyncStatus(dc *nacosiov1.DynamicConfiguration, dataId string) {
	if dc == nil {
		return
	}
	var newStatus []nacosiov1.SyncStatus
	for _, status := range dc.Status.SyncStatuses {
		if dataId == status.DataId {
			continue
		}
		newStatus = append(newStatus, status)
	}
	dc.Status.SyncStatuses = newStatus
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

func GetSyncStatusByDataId(statuses []nacosiov1.SyncStatus, dataId string) *nacosiov1.SyncStatus {
	if len(statuses) == 0 {
		return nil
	}
	for i := range statuses {
		v := statuses[i]
		if v.DataId == dataId {
			return &v
		}
	}
	return nil
}
