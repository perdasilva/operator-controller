package variables

import (
	"encoding/json"
	"fmt"
	"github.com/perdasilva/replee/pkg/deppy"
	"github.com/perdasilva/replee/pkg/deppy/constraints"
	"github.com/perdasilva/replee/pkg/deppy/utils"
	"reflect"
	"sync"
)

var _ deppy.MutableVariable = &MutableVariable{}

type MutableVariable struct {
	variableID  deppy.Identifier
	kind        string
	lock        sync.RWMutex
	properties  map[string]interface{}
	constraints *utils.ActivationMap[deppy.Identifier, deppy.Constraint]
}

func (v *MutableVariable) GetConstraint(constraintID deppy.Identifier) (deppy.Constraint, bool) {
	return v.constraints.GetValue(constraintID)
}

func (v *MutableVariable) GetConstraintIDs() []deppy.Identifier {
	return v.constraints.Keys()
}

func (v *MutableVariable) Merge(other deppy.Variable) error {
	v.lock.Lock()
	defer v.lock.Unlock()

	if v.Kind() != other.Kind() {
		return deppy.ConflictErrorf("variable %s is not of kind %s", other.Identifier(), v.Kind())
	}

	// merge properties
	for key, value := range other.GetProperties() {
		if err := v.setProperty(key, value); err != nil {
			return err
		}
	}

	// merge constraints
	for _, constraintID := range other.GetConstraintIDs() {
		oc, _ := other.GetConstraint(constraintID)
		if !v.HasConstraint(constraintID) {
			v.constraints.Put(constraintID, oc)
			if isActivated, err := other.IsActivated(constraintID); !isActivated && err == nil {
				v.constraints.Deactivate(constraintID)
			}
		} else {
			c, _ := v.GetConstraint(constraintID)
			if mc, ok := c.(deppy.MutableConstraint); ok {
				if err := mc.Merge(oc); err != nil {
					return err
				}
			} else {
				return deppy.ConflictErrorf("merge error: constraint %s is not mutable", constraintID)
			}
		}
	}

	return nil
}

func NewMutableVariable(variableID deppy.Identifier, kind string, properties map[string]interface{}) deppy.MutableVariable {
	if properties == nil {
		properties = map[string]interface{}{}
	}
	return &MutableVariable{
		variableID:  variableID,
		kind:        kind,
		properties:  properties,
		constraints: utils.NewActivationMap[deppy.Identifier, deppy.Constraint](),
		lock:        sync.RWMutex{},
	}
}

func (v *MutableVariable) String() string {
	str, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("error marshaling variable: %s", err)
	}
	return string(str)
}

func (v *MutableVariable) GetProperties() map[string]interface{} {
	v.lock.RLock()
	defer v.lock.RUnlock()
	return v.properties
}

func (v *MutableVariable) GetProperty(key string) (interface{}, bool) {
	v.lock.RLock()
	defer v.lock.RUnlock()
	prop, ok := v.properties[key]
	return prop, ok
}

func (v *MutableVariable) SetProperty(key string, value interface{}) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	return v.setProperty(key, value)
}

func (v *MutableVariable) setProperty(key string, value interface{}) error {
	if curValue, ok := v.properties[key]; ok {
		if reflect.DeepEqual(curValue, value) {
			return nil
		}
		return deppy.ConflictErrorf("merge conflict: property %s already set to %v", key, curValue)
	}
	v.properties[key] = value
	return nil
}

func (v *MutableVariable) Kind() string {
	return v.kind
}

func (v *MutableVariable) Identifier() deppy.Identifier {
	return v.variableID
}

func (v *MutableVariable) Constraints() []deppy.Constraint {
	v.lock.RLock()
	defer v.lock.RUnlock()
	var cs []deppy.Constraint
	for _, c := range v.constraints.Elements() {
		cs = append(cs, c)
	}
	return cs
}

func (v *MutableVariable) AddMandatory(constraintID deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if _, ok := v.constraints.GetValue(constraintID); !ok {
		v.constraints.Put(constraintID, constraints.Mandatory(constraintID))
	}
	v.constraints.Activate(constraintID)
	return nil
}

func (v *MutableVariable) RemoveMandatory(constraintID deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if _, ok := v.constraints.GetValue(constraintID); !ok {
		v.constraints.Put(constraintID, constraints.Mandatory(constraintID))
	}
	v.constraints.Deactivate(constraintID)
	return nil
}

func (v *MutableVariable) AddProhibited(constraintID deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if _, ok := v.constraints.GetValue(constraintID); !ok {
		v.constraints.Put(constraintID, constraints.Prohibited(constraintID))
	}
	v.constraints.Activate(constraintID)
	return nil
}

func (v *MutableVariable) RemoveProhibited(constraintID deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if _, ok := v.constraints.GetValue(constraintID); !ok {
		v.constraints.Put(constraintID, constraints.Prohibited(constraintID))
	}
	v.constraints.Deactivate(constraintID)
	return nil
}

func (v *MutableVariable) AddConflict(constraintID deppy.Identifier, conflictingVariableID deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if c, ok := v.constraints.GetValue(constraintID); !ok {
		v.constraints.Put(constraintID, constraints.Conflict(constraintID, conflictingVariableID))
	} else if _, ok := c.(*constraints.ConflictConstraint); !ok {
		return deppy.FatalError(fmt.Sprintf("constraint with id %s is not a Conflict constraint", constraintID))
	}
	c, _ := v.constraints.GetValue(constraintID)
	if err := c.(*constraints.ConflictConstraint).SetConflictingVariableID(conflictingVariableID); err != nil {
		return err
	}
	v.constraints.Activate(constraintID)
	return nil
}

func (v *MutableVariable) RemoveConflict(constraintID deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if c, ok := v.constraints.GetValue(constraintID); !ok {
		v.constraints.Put(constraintID, constraints.Conflict(constraintID, ""))
	} else if _, ok := c.(*constraints.ConflictConstraint); !ok {
		return deppy.FatalError(fmt.Sprintf("constraint with id %s is not a Conflict constraint", constraintID))
	}
	v.constraints.Deactivate(constraintID)
	return nil
}

func (v *MutableVariable) AddDependency(constraintID deppy.Identifier, dependentVariableIDs ...deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if _, ok := v.constraints.GetValue(constraintID); !ok {
		dep := constraints.Dependency(constraintID, dependentVariableIDs...)
		v.constraints.Put(constraintID, dep)
		return nil
	}

	if d, ok := v.constraints.MustGet(constraintID).(*constraints.DependencyConstraint); !ok {
		return deppy.FatalError(fmt.Sprintf("constraint with id %s is not a Dependency constraint", constraintID))
	} else {
		d.Activate(dependentVariableIDs...)
	}
	return nil
}

func (v *MutableVariable) RemoveDependencyConstraint(constraintID deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if c, ok := v.constraints.GetValue(constraintID); !ok {
		v.constraints.Put(constraintID, constraints.Dependency(constraintID))
	} else if _, ok := c.(*constraints.DependencyConstraint); !ok {
		return deppy.FatalError(fmt.Sprintf("constraint with id %s is not a Dependency constraint", constraintID))
	}
	v.constraints.Deactivate(constraintID)
	return nil
}

func (v *MutableVariable) RemoveDependency(constraintID deppy.Identifier, dependentVariableIDs ...deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if _, ok := v.constraints.GetValue(constraintID); !ok {
		dep := constraints.Dependency(constraintID)
		v.constraints.Put(constraintID, dep)
	}

	if d, ok := v.constraints.MustGet(constraintID).(*constraints.DependencyConstraint); !ok {
		return deppy.FatalError(fmt.Sprintf("constraint with id %s is not a Dependency constraint", constraintID))
	} else {
		if len(dependentVariableIDs) == 0 {
			v.constraints.Deactivate(constraintID)
		} else {
			for _, dependentVariableID := range dependentVariableIDs {
				d.Deactivate(dependentVariableID)
			}
		}
	}

	c, _ := v.constraints.GetValue(constraintID)
	dep, _ := c.(*constraints.DependencyConstraint)
	for _, dependentVariableID := range dependentVariableIDs {
		dep.Deactivate(dependentVariableID)
	}
	return nil
}

func (v *MutableVariable) AddAtMost(constraintID deppy.Identifier, n int, variableIDs ...deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if _, ok := v.constraints.GetValue(constraintID); !ok {
		atMost := constraints.AtMost(constraintID, n, variableIDs...)
		v.constraints.Put(constraintID, atMost)
		return nil
	}

	if a, ok := v.constraints.MustGet(constraintID).(*constraints.AtMostConstraint); !ok {
		return deppy.FatalError(fmt.Sprintf("constraint with id %s is not an AtMost constraint", constraintID))
	} else {
		a.Activate(variableIDs...)
	}
	v.constraints.Activate(constraintID)
	return nil
}

func (v *MutableVariable) RemoveAtMost(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	if _, ok := v.constraints.GetValue(constraintID); !ok {
		atMost := constraints.AtMost(constraintID, -1)
		v.constraints.Put(constraintID, atMost)
	}

	if a, ok := v.constraints.MustGet(constraintID).(*constraints.AtMostConstraint); !ok {
		return deppy.FatalError(fmt.Sprintf("constraint with id %s is not an AtMost constraint", constraintID))
	} else {
		if len(variableIDs) == 0 {
			v.constraints.Deactivate(constraintID)
		} else {
			a.Deactivate(variableIDs...)
		}
	}
	return nil
}

func (v *MutableVariable) SetAtMostN(constraintID deppy.Identifier, n int) error {
	v.lock.Lock()
	defer v.lock.Unlock()
	var atMost *constraints.AtMostConstraint
	c, ok := v.constraints.GetValue(constraintID)
	if !ok {
		atMost = constraints.AtMost(constraintID, n)
		v.constraints.Put(constraintID, atMost)
	} else {
		var ok bool
		atMost, ok = c.(*constraints.AtMostConstraint)
		if !ok {
			return deppy.FatalError(fmt.Sprintf("constraint with id %s is not an AtMost constraint", constraintID))
		}
	}

	if err := atMost.SetN(n); err != nil {
		return err
	}
	return nil
}

func (v *MutableVariable) HasConstraint(constraintID deppy.Identifier) bool {
	return v.constraints.Has(constraintID)
}

func (v *MutableVariable) IsActivated(constraintID deppy.Identifier) (bool, error) {
	return v.constraints.IsActivated(constraintID)
}

func (v *MutableVariable) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		VariableID  deppy.Identifier                                         `json:"variableID"`
		Kind        string                                                   `json:"kind"`
		Properties  map[string]interface{}                                   `json:"properties"`
		Constraints *utils.ActivationMap[deppy.Identifier, deppy.Constraint] `json:"constraints"`
	}{
		VariableID:  v.variableID,
		Kind:        v.kind,
		Properties:  v.properties,
		Constraints: v.constraints,
	})
}

func (v *MutableVariable) UnmarshalJSON(jsonBytes []byte) error {
	data := &struct {
		VariableID  deppy.Identifier                                        `json:"variableID"`
		Kind        string                                                  `json:"kind"`
		Properties  map[string]interface{}                                  `json:"properties"`
		Constraints *utils.ActivationMap[deppy.Identifier, json.RawMessage] `json:"constraints"`
	}{}
	if err := json.Unmarshal(jsonBytes, data); err != nil {
		return err
	}
	v.variableID = data.VariableID
	v.kind = data.Kind
	v.properties = data.Properties
	v.constraints = utils.NewActivationMap[deppy.Identifier, deppy.Constraint]()
	for _, constraintID := range data.Constraints.Keys() {
		constraintBytes, _ := data.Constraints.GetValue(constraintID)
		mc := &constraints.MutableConstraintBase{}
		if err := json.Unmarshal(constraintBytes, mc); err != nil {
			return err
		}
		var c deppy.Constraint
		switch mc.Kind() {
		case constraints.ConstraintKindMandatory:
			c = &constraints.MandatoryConstraint{}
		case constraints.ConstraintKindProhibited:
			c = &constraints.ProhibitedConstraint{}
		case constraints.ConstraintKindConflict:
			c = &constraints.ConflictConstraint{}
		case constraints.ConstraintKindDependency:
			c = &constraints.DependencyConstraint{}
		case constraints.ConstraintKindAtMost:
			c = &constraints.AtMostConstraint{}
		default:
			return deppy.Fatalf("unknown constraint kind %s", mc.Kind())
		}
		if err := json.Unmarshal(constraintBytes, &c); err != nil {
			return err
		}
		v.constraints.Put(constraintID, c)
		if activated, err := data.Constraints.IsActivated(constraintID); err != nil {
			return nil
		} else if !activated {
			v.constraints.Deactivate(constraintID)
		}
	}
	return nil
}

//func (v *MutableVariable) RemoveConflictWithAny(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) AddConflictsWithAll(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) RemoveConflictsWithAll(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) AddDependsOn(variableID deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) RemoveDependsOn(variableID deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) AddDependsOnAny(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) RemoveDependsOnAny(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) AddDependsOnAll(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) RemoveDependsOnAll(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (v *MutableVariable) AddConflictsWithAny(constraintID deppy.Identifier, variableIDs ...deppy.Identifier) error {
//	//TODO implement me
//	panic("implement me")
//}
