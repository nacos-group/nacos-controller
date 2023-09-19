package nacos

import (
	"k8s.io/apimachinery/pkg/types"
	"sync"
)

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
