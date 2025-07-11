package storage

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Persistent interface {
	Lock()
	Unlock()
	IsUpdated() bool //更新标志位
}

const (
	defaultSaveInterval = 5 * time.Second
	maxRetryAttempts    = 3
	retryDelay          = 100 * time.Millisecond
)

type Persister struct {
	filePath    string
	Obj         Persistent // 保存的对象
	mu          sync.Mutex
	lastSaveErr error
}

func NewPersister(filePath string, obj Persistent) (*Persister, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, wrapError(err, "create storage directory failed")
	}

	p := &Persister{
		filePath: filePath,
		Obj:      obj,
	}

	// 尝试加载数据
	if err := p.Load(obj); err != nil {
		slog.Error(wrapError(err, "load data failed").Error())
	}

	go p.autoSave()
	return p, nil
}

func (p *Persister) Save(data Persistent) error {
	data.Lock()
	defer data.Unlock()

	return p.saveWithRetry(data)
}

func (p *Persister) Load(data Persistent) error {
	data.Lock()
	defer data.Unlock()

	return p.loadWithRetry(data)
}

func (p *Persister) autoSave() {
	ticker := time.NewTicker(defaultSaveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			if p.Obj.IsUpdated() {
				p.lastSaveErr = p.saveWithRetry(p.Obj)
			}
			p.mu.Unlock()
		}
	}
}

func (p *Persister) saveWithRetry(data interface{}) error {
	var err error
	for i := 0; i < maxRetryAttempts; i++ {
		if err = p.atomicSave(data); err == nil {
			return nil
		}
		time.Sleep(retryDelay)
	}
	return wrapError(err, "save failed after retries")
}

func (p *Persister) atomicSave(data interface{}) error {
	tmpFile := p.filePath + ".tmp"

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(tmpFile, jsonData, 0644); err != nil {
		return err
	}

	return os.Rename(tmpFile, p.filePath)
}

func (p *Persister) loadWithRetry(data interface{}) error {
	var err error
	for i := 0; i < maxRetryAttempts; i++ {
		if err = p.atomicLoad(data); err == nil {
			return nil
		}
		time.Sleep(retryDelay)
	}
	return wrapError(err, "load failed after retries")
}

func (p *Persister) atomicLoad(data interface{}) error {
	fileData, err := os.ReadFile(p.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return json.Unmarshal(fileData, data)
}

func wrapError(err error, msg string) error {
	if err == nil {
		return nil
	}
	return errors.New(msg + ": " + err.Error())
}
