package utils

import (
	"encoding/json"
	"github.com/perdasilva/replee/pkg/deppy"
	"sort"
	"sync"
	"sync/atomic"
)

type ActivationValue[T any] struct {
	value T
	count int64
}

func NewActivationVariable[T any](value T) *ActivationValue[T] {
	return &ActivationValue[T]{
		value: value,
		count: 1, // values start activated
	}
}

func (a *ActivationValue[T]) MarshalJSON() ([]byte, error) {
	bytes, err := json.Marshal(&struct {
		Value       T    `json:"value"`
		IsActivated bool `json:"activated"`
	}{
		Value:       a.value,
		IsActivated: a.IsActivated(),
	})
	return bytes, err
}

func (a *ActivationValue[T]) UnmarshalJSON(jsonBytes []byte) error {
	data := &struct {
		Value       T    `json:"value"`
		IsActivated bool `json:"activated"`
	}{}
	if err := json.Unmarshal(jsonBytes, data); err != nil {
		return err
	}
	a.value = data.Value
	if !data.IsActivated {
		a.count = 0
	} else {
		a.count = 1
	}
	return nil
}

func (a *ActivationValue[T]) Value() T {
	return a.value
}

func (a *ActivationValue[T]) Count() int64 {
	return atomic.LoadInt64(&a.count)
}

func (a *ActivationValue[T]) IsActivated() bool {
	return atomic.LoadInt64(&a.count) > 0
}

func (a *ActivationValue[T]) Activate() {
	atomic.AddInt64(&a.count, 1)
}

func (a *ActivationValue[T]) Deactivate() {
	atomic.AddInt64(&a.count, -1)
}

type SortFn func(a, b int) bool

type ActivationSet[T comparable] struct {
	values map[T]*ActivationValue[T]
	lock   sync.RWMutex
	sortFn SortFn
}

func NewActivationSet[T comparable](keys ...T) *ActivationSet[T] {
	activationSet := &ActivationSet[T]{
		values: map[T]*ActivationValue[T]{},
		lock:   sync.RWMutex{},
	}
	activationSet.Add(keys...)
	return activationSet
}

func (a *ActivationSet[T]) Add(keys ...T) {
	a.lock.Lock()
	defer a.lock.Unlock()
	for _, key := range keys {
		a.add(key)
	}
}

func (a *ActivationSet[T]) add(key T) {
	a.values[key] = NewActivationVariable(key)
}

func (a *ActivationSet[T]) Activate(keys ...T) {
	a.lock.Lock()
	defer a.lock.Unlock()
	for _, key := range keys {
		if _, ok := a.values[key]; !ok {
			a.add(key)
		}
	}
}

func (a *ActivationSet[T]) IsActivated(key T) (bool, error) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	if a.Has(key) {
		return a.values[key].IsActivated(), nil
	}
	return false, deppy.NotFoundErrorf("key not found")
}

func (a *ActivationSet[T]) Deactivate(keys ...T) {
	a.lock.Lock()
	defer a.lock.Unlock()
	for _, key := range keys {
		if _, ok := a.values[key]; !ok {
			a.add(key)
		}
		a.values[key].Deactivate()
	}
}

func (a *ActivationSet[T]) Elements() []T {
	a.lock.RLock()
	defer a.lock.RUnlock()
	var elements []T
	for _, value := range a.values {
		if value.IsActivated() {
			elements = append(elements, value.Value())
		}
	}

	if a.sortFn != nil {
		sort.SliceStable(elements, a.sortFn)
	}
	return elements
}

func (a *ActivationSet[T]) Has(key T) bool {
	a.lock.RLock()
	defer a.lock.RUnlock()
	_, ok := a.values[key]
	return ok
}

func (a *ActivationSet[T]) MarshalJSON() ([]byte, error) {
	out := make(map[T]bool, len(a.values))
	for _, value := range a.values {
		out[value.Value()] = value.IsActivated()
	}
	return json.Marshal(out)
}

func (a *ActivationSet[T]) UnmarshalJSON(jsonBytes []byte) error {
	data := make(map[T]bool, 0)
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return err
	}
	a.values = make(map[T]*ActivationValue[T], len(data))

	for value, activated := range data {
		a.Add(value)
		if !activated {
			a.Deactivate(value)
		}
	}
	return nil
}

type ActivationMap[K comparable, V any] struct {
	values map[K]*ActivationValue[V]
	lock   sync.RWMutex
	sortFn SortFn
}

func NewActivationMap[K comparable, V any]() *ActivationMap[K, V] {
	return &ActivationMap[K, V]{
		values: map[K]*ActivationValue[V]{},
		lock:   sync.RWMutex{},
	}
}

func (a *ActivationMap[K, V]) Len() int {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return len(a.values)
}

func (a *ActivationMap[K, V]) Put(key K, value V) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.values[key] = NewActivationVariable[V](value)
}

func (a *ActivationMap[K, V]) GetValue(key K) (V, bool) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	if value, ok := a.values[key]; ok {
		return value.Value(), true
	}
	return *new(V), false
}

func (a *ActivationMap[K, V]) Elements() []V {
	a.lock.RLock()
	defer a.lock.RUnlock()
	var elements []V
	for _, value := range a.values {
		if value.IsActivated() {
			elements = append(elements, value.Value())
		}
	}

	if a.sortFn != nil {
		sort.SliceStable(elements, a.sortFn)
	}
	return elements
}

func (a *ActivationMap[K, V]) Keys() []K {
	a.lock.RLock()
	defer a.lock.RUnlock()
	var keys []K
	for key := range a.values {
		keys = append(keys, key)
	}
	return keys
}

func (a *ActivationMap[K, V]) Activate(key K) {
	a.lock.Lock()
	defer a.lock.Unlock()
	if _, ok := a.values[key]; ok {
		a.values[key].Activate()
	}
}

func (a *ActivationMap[K, V]) Deactivate(key K) {
	a.lock.Lock()
	defer a.lock.Unlock()
	if _, ok := a.values[key]; ok {
		a.values[key].Deactivate()
	}
}

func (a *ActivationMap[K, V]) MustGet(key K) V {
	if value, ok := a.GetValue(key); ok {
		return value
	}
	panic("key not found")
}

func (a *ActivationMap[K, V]) Has(key K) bool {
	_, ok := a.GetValue(key)
	return ok
}

func (a *ActivationMap[K, V]) IsActivated(key K) (bool, error) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	v, ok := a.values[key]
	if !ok {
		return false, deppy.NotFoundErrorf("key not found")
	}
	return v.IsActivated(), nil
}

func (a *ActivationMap[K, V]) MarshalJSON() ([]byte, error) {
	bytes, err := json.Marshal(a.values)
	return bytes, err
}

func (a *ActivationMap[K, V]) UnmarshalJSON(jsonBytes []byte) error {
	data := make(map[K]ActivationValue[V], 0)
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return err
	}

	if a.values == nil {
		a.values = map[K]*ActivationValue[V]{}
	}
	for key, _ := range data {
		val := data[key]
		a.values[key] = &val
	}

	return nil
}
