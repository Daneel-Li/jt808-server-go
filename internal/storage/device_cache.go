package storage

import (
	"errors"
	"sync"

	"golang.org/x/exp/maps"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

var ErrDeviceNotFound = errors.New("device not found")

type DeviceCache struct {
	CacheByPlate map[string]*model.Device
	CacheByPhone map[string]*model.Device
	mutex        *sync.Mutex
	updated      bool
}

var deviceCacheSingleton *DeviceCache
var deviceCacheInitOnce sync.Once

func GetDeviceCache() *DeviceCache {
	deviceCacheInitOnce.Do(func() {
		deviceCacheSingleton = &DeviceCache{
			CacheByPlate: make(map[string]*model.Device),
			CacheByPhone: make(map[string]*model.Device),
			mutex:        &sync.Mutex{},
		}
		NewPersister("device_cache.json", deviceCacheSingleton) //启动自动持久化
	})
	return deviceCacheSingleton
}

func (cache *DeviceCache) Lock() {
	cache.mutex.Lock()
}
func (cache *DeviceCache) Unlock() {
	cache.mutex.Unlock()
}
func (cache *DeviceCache) IsUpdated() bool {
	return cache.updated
}

func (cache *DeviceCache) ListDevice() []*model.Device {
	return maps.Values(cache.CacheByPhone)
}

func (cache *DeviceCache) GetDeviceByPlate(carPlate string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.CacheByPlate[carPlate]; ok {
		return d, nil
	}
	return nil, ErrDeviceNotFound
}

func (cache *DeviceCache) HasPlate(carPlate string) bool {
	d, err := cache.GetDeviceByPlate(carPlate)
	return d != nil && err == nil
}

func (cache *DeviceCache) GetDeviceByPhone(phone string) (*model.Device, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if d, ok := cache.CacheByPhone[phone]; ok {
		return d, nil
	}
	return nil, ErrDeviceNotFound
}

func (cache *DeviceCache) HasPhone(phone string) bool {
	d, err := cache.GetDeviceByPhone(phone)
	return d != nil && err == nil
}

func (cache *DeviceCache) cacheDevice(d *model.Device) {
	cache.CacheByPlate[d.Plate] = d
	cache.CacheByPhone[d.Phone] = d
}

func (cache *DeviceCache) CacheDevice(d *model.Device) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	defer func() { cache.updated = true }()
	cache.cacheDevice(d)
}

func (cache *DeviceCache) delDevice(carPlate, phone *string) {
	var d *model.Device
	var ok bool
	if carPlate != nil {
		d, ok = cache.CacheByPlate[*carPlate]
	}
	if phone != nil {
		d, ok = cache.CacheByPhone[*phone]
	}
	if !ok {
		return // find none device, skip
	}
	delete(cache.CacheByPlate, d.Plate)
	delete(cache.CacheByPhone, d.Phone)
}

func (cache *DeviceCache) DelDeviceByCarPlate(carPlate string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.delDevice(&carPlate, nil)
}

func (cache *DeviceCache) DelDeviceByPhone(phone string) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.delDevice(nil, &phone)
}
