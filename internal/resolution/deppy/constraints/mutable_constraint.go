package constraints

import (
	"encoding/json"
	"fmt"
	"github.com/perdasilva/replee/pkg/deppy"
	"sync"
)

type MutableConstraintBase struct {
	constraintID deppy.Identifier
	kind         string
	properties   map[string]interface{}
	lock         sync.RWMutex
}

func (m *MutableConstraintBase) ConstraintID() deppy.Identifier {
	return m.constraintID
}

func (m *MutableConstraintBase) Kind() string {
	return m.kind
}

func (m *MutableConstraintBase) Merge(other deppy.Constraint) error {
	for key, value := range other.GetProperties() {
		if err := m.setProperty(key, value); err != nil {
			return err
		}
	}
	return nil
}

func (m *MutableConstraintBase) String() string {
	str, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Sprintf("error marshaling constraint: %s", err)
	}
	return string(str)
}

func (m *MutableConstraintBase) GetProperty(key string) (interface{}, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	prop, ok := m.properties[key]
	return prop, ok
}

func (m *MutableConstraintBase) GetProperties() map[string]interface{} {
	m.lock.RLock()
	defer m.lock.RUnlock()
	props := map[string]interface{}{}
	for k, v := range m.properties {
		props[k] = v
	}
	return props
}

func (m *MutableConstraintBase) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		ConstraintID deppy.Identifier       `json:"constraintID"`
		Kind         string                 `json:"kind"`
		Properties   map[string]interface{} `json:"properties"`
	}{
		ConstraintID: m.ConstraintID(),
		Kind:         m.Kind(),
		Properties:   m.GetProperties(),
	})
}

func (m *MutableConstraintBase) UnmarshalJSON(jsonBytes []byte) error {
	data := &struct {
		ConstraintID deppy.Identifier       `json:"constraintID"`
		Kind         string                 `json:"kind"`
		Properties   map[string]interface{} `json:"properties"`
	}{}
	if err := json.Unmarshal(jsonBytes, data); err != nil {
		return err
	}
	m.constraintID = data.ConstraintID
	m.kind = data.Kind
	m.properties = data.Properties
	return nil
}

func (m *MutableConstraintBase) SetProperty(key string, value interface{}) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.setProperty(key, value)
}

func (m *MutableConstraintBase) setProperty(key string, value interface{}) error {
	if v, ok := m.properties[key]; ok {
		if v == value {
			return nil
		}
		return deppy.ConflictErrorf("merge conflict: property %s already set to %v", key, v)
	}
	m.properties[key] = value
	return nil
}
