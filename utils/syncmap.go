package utils

import "sync"

type SyncMap[K comparable, V any] struct {
	sm sync.Map
}

func NewSyncMap[K comparable, V any]() *SyncMap[K, V] {
	return &SyncMap[K, V]{
		sm: sync.Map{},
	}
}

func (m *SyncMap[K, V]) Store(key K, value V) {
	m.sm.Store(key, value)
}

func (m *SyncMap[K, V]) Load(key K) (V, bool) {
	val, ok := m.sm.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return val.(V), ok
}

func (m *SyncMap[K, V]) LoadOrStore(key K, value V) (V, bool) {
	val, ok := m.sm.LoadOrStore(key, value)
	return val.(V), ok
}

func (m *SyncMap[K, V]) Delete(key K) {
	m.sm.Delete(key)
}

func (m *SyncMap[K, V]) Range(f func(key K, value V) bool) {
	m.sm.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}

func (m *SyncMap[K, V]) LoadAndDelete(key K) (V, bool) {
	val, ok := m.sm.LoadAndDelete(key)
	if !ok {
		var zero V
		return zero, false
	}
	return val.(V), ok
}

func (m *SyncMap[K, V]) Swap(key K, value V) (V, bool) {
	val, ok := m.sm.Swap(key, value)
	if !ok {
		var zero V
		return zero, false
	}
	return val.(V), ok
}

func (m *SyncMap[K, V]) Clear() {
	m.sm.Clear()
}
