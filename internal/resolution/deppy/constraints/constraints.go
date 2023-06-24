package constraints

import (
	"encoding/json"
	"fmt"
	"github.com/go-air/gini/z"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/utils"
	"strings"
	"sync"
)

const (
	ConstraintKindMandatory  = "deppy.constraint.mandatory"
	ConstraintKindProhibited = "deppy.constraint.prohibited"
	ConstraintKindConflict   = "deppy.constraint.conflict"
	ConstraintKindDependency = "deppy.constraint.dependency"
	ConstraintKindAtMost     = "deppy.constraint.atmost"
)

type Constraint deppy.Constraint

var _ deppy.Constraint = &MandatoryConstraint{}

type MandatoryConstraint struct {
	MutableConstraintBase
}

func Mandatory(constraintID deppy.Identifier) *MandatoryConstraint {
	return &MandatoryConstraint{
		MutableConstraintBase: MutableConstraintBase{
			constraintID: constraintID,
			kind:         ConstraintKindMandatory,
			properties:   map[string]interface{}{},
			lock:         sync.RWMutex{},
		},
	}
}

func (constraint *MandatoryConstraint) String(subject deppy.Identifier) string {
	return fmt.Sprintf("%s is mandatory", subject)
}

func (constraint *MandatoryConstraint) Apply(lm deppy.LitMapping, subject deppy.Identifier) z.Lit {
	return lm.LitOf(subject)
}

func (constraint *MandatoryConstraint) Order() []deppy.Identifier {
	return nil
}

func (constraint *MandatoryConstraint) Anchor() bool {
	return true
}

func (constraint *MandatoryConstraint) Merge(other deppy.Constraint) (bool, error) {
	if _, ok := other.(*MandatoryConstraint); !ok {
		return false, deppy.ConflictErrorf("cannot merge constraints of different kind [%T != %T]", constraint, other)
	}
	return constraint.MutableConstraintBase.Merge(other)
}

var _ deppy.Constraint = &ProhibitedConstraint{}

type ProhibitedConstraint struct {
	MutableConstraintBase
}

func Prohibited(constraintID deppy.Identifier) *ProhibitedConstraint {
	return &ProhibitedConstraint{
		MutableConstraintBase: MutableConstraintBase{
			constraintID: constraintID,
			kind:         ConstraintKindProhibited,
			properties:   map[string]interface{}{},
			lock:         sync.RWMutex{},
		},
	}
}

func (constraint *ProhibitedConstraint) Merge(other deppy.Constraint) (bool, error) {
	if _, ok := other.(*ProhibitedConstraint); !ok {
		return false, deppy.ConflictErrorf("cannot merge constraints of different kind [%T != %T]", constraint, other)
	}
	return constraint.MutableConstraintBase.Merge(other)
}

func (constraint *ProhibitedConstraint) String(subject deppy.Identifier) string {
	return fmt.Sprintf("%s is ProhibitedConstraint", subject)
}

func (constraint *ProhibitedConstraint) Apply(lm deppy.LitMapping, subject deppy.Identifier) z.Lit {
	return lm.LitOf(subject).Not()
}

func (constraint *ProhibitedConstraint) Order() []deppy.Identifier {
	return nil
}

func (constraint *ProhibitedConstraint) Anchor() bool {
	return false
}

var _ deppy.Constraint = &ConflictConstraint{}

type ConflictConstraint struct {
	MutableConstraintBase
	conflictingVariableID deppy.Identifier
	lock                  sync.RWMutex
}

func Conflict(constraintID deppy.Identifier, conflict deppy.Identifier) *ConflictConstraint {
	return &ConflictConstraint{
		MutableConstraintBase: MutableConstraintBase{
			constraintID: constraintID,
			kind:         ConstraintKindConflict,
			properties:   map[string]interface{}{},
			lock:         sync.RWMutex{},
		},
		conflictingVariableID: conflict,
	}
}

func (constraint *ConflictConstraint) Merge(other deppy.Constraint) (bool, error) {
	if cc, ok := other.(*ConflictConstraint); ok {
		if cc.conflictingVariableID != constraint.conflictingVariableID {
			return false, deppy.ConflictErrorf("cannot merge constraints with different conflicting variable [%s != %s]", constraint.conflictingVariableID, cc.conflictingVariableID)
		}
	}
	if constraint.conflictingVariableID != other.(*ConflictConstraint).conflictingVariableID {
		return false, deppy.ConflictErrorf("cannot merge constraints with different conflicting variable [%s != %s]", constraint.conflictingVariableID, other.(*ConflictConstraint).conflictingVariableID)
	}

	return constraint.MutableConstraintBase.Merge(other)
}

func (constraint *ConflictConstraint) String(subject deppy.Identifier) string {
	return fmt.Sprintf("%s conflicts with %s", subject, constraint.conflictingVariableID)
}

func (constraint *ConflictConstraint) Apply(lm deppy.LitMapping, subject deppy.Identifier) z.Lit {
	return lm.LogicCircuit().Or(lm.LitOf(subject).Not(), lm.LitOf(constraint.conflictingVariableID).Not())
}

func (constraint *ConflictConstraint) Order() []deppy.Identifier {
	return nil
}

func (constraint *ConflictConstraint) Anchor() bool {
	return false
}

func (constraint *ConflictConstraint) SetConflictingVariableID(id deppy.Identifier) error {
	constraint.lock.Lock()
	defer constraint.lock.Unlock()
	if constraint.conflictingVariableID == "" || constraint.conflictingVariableID == id {
		constraint.conflictingVariableID = id
		return nil
	}
	return deppy.FatalError("conflicting variable id already set")
}

func (constraint *ConflictConstraint) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind               string                 `json:"kind"`
		Properties         map[string]interface{} `json:"properties"`
		ConflictVariableID deppy.Identifier       `json:"conflictVariableID"`
	}{
		Kind:               constraint.Kind(),
		Properties:         constraint.GetProperties(),
		ConflictVariableID: constraint.conflictingVariableID,
	})
}

func (constraint *ConflictConstraint) UnmarshalJSON(jsonBytes []byte) error {
	data := &struct {
		Kind               string                 `json:"kind"`
		Properties         map[string]interface{} `json:"properties"`
		ConflictVariableID deppy.Identifier       `json:"conflictVariableID"`
	}{}
	if err := json.Unmarshal(jsonBytes, data); err != nil {
		return err
	}
	constraint.kind = data.Kind
	constraint.properties = data.Properties
	constraint.conflictingVariableID = data.ConflictVariableID
	return nil
}

var _ deppy.Constraint = &DependencyConstraint{}

type DependencyConstraint struct {
	MutableConstraintBase
	*utils.ActivationSet[deppy.Identifier]
}

func Dependency(constraintID deppy.Identifier, dependencies ...deppy.Identifier) *DependencyConstraint {
	c := &DependencyConstraint{
		MutableConstraintBase: MutableConstraintBase{
			constraintID: constraintID,
			kind:         ConstraintKindDependency,
			properties:   map[string]interface{}{},
			lock:         sync.RWMutex{},
		},
		ActivationSet: utils.NewActivationSet[deppy.Identifier](),
	}
	for _, dependency := range dependencies {
		c.Activate(dependency)
	}
	return c
}

func (constraint *DependencyConstraint) Merge(other deppy.Constraint) (bool, error) {
	changed := false
	if cc, ok := other.(*DependencyConstraint); ok {
		if constraint.ActivationSet == nil {
			constraint.ActivationSet = utils.NewActivationSet[deppy.Identifier]()
		}
		ok, err := constraint.ActivationSet.Merge(cc.ActivationSet)
		if err != nil {
			return false, err
		}
		changed = changed || ok
	} else if !ok {
		return false, deppy.ConflictErrorf("cannot merge constraints of different kind [%T != %T]", constraint, other)
	}
	ok, err := constraint.MutableConstraintBase.Merge(other)
	if err != nil {
		return false, err
	}
	return changed || ok, nil
}

func (constraint *DependencyConstraint) Apply(lm deppy.LitMapping, subject deppy.Identifier) z.Lit {
	dependencyIDs := constraint.Elements()
	m := lm.LitOf(subject).Not()
	for _, each := range dependencyIDs {
		m = lm.LogicCircuit().Or(m, lm.LitOf(each))
	}
	return m
}

func (constraint *DependencyConstraint) Order() []deppy.Identifier {
	return constraint.Elements()
}

func (constraint *DependencyConstraint) Anchor() bool {
	return false
}

func (constraint *DependencyConstraint) String(subject deppy.Identifier) string {
	dependencyIDs := constraint.Elements()
	if len(dependencyIDs) == 0 {
		return fmt.Sprintf("%s has a DependencyConstraint without any candidates to satisfy it", subject)
	}
	s := make([]string, len(dependencyIDs))
	for i, each := range dependencyIDs {
		s[i] = string(each)
	}
	return fmt.Sprintf("%s requires at least one of %s", subject, strings.Join(s, ", "))
}

func (constraint *DependencyConstraint) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind          string                                 `json:"kind"`
		Properties    map[string]interface{}                 `json:"properties"`
		DependencyIDs *utils.ActivationSet[deppy.Identifier] `json:"dependencyIDs"`
	}{
		Kind:          constraint.Kind(),
		Properties:    constraint.GetProperties(),
		DependencyIDs: constraint.ActivationSet,
	})
}

func (constraint *DependencyConstraint) UnmarshalJSON(jsonBytes []byte) error {
	data := &struct {
		Kind          string                                 `json:"kind"`
		Properties    map[string]interface{}                 `json:"properties"`
		DependencyIDs *utils.ActivationSet[deppy.Identifier] `json:"dependencyIDs"`
	}{}
	if err := json.Unmarshal(jsonBytes, data); err != nil {
		return err
	}
	constraint.kind = data.Kind
	constraint.properties = data.Properties
	constraint.ActivationSet = data.DependencyIDs
	return nil
}

var _ deppy.Constraint = &AtMostConstraint{}

type AtMostConstraint struct {
	MutableConstraintBase
	*utils.ActivationSet[deppy.Identifier]
	n    int
	lock sync.RWMutex
}

func AtMost(constraintID deppy.Identifier, n int, variables ...deppy.Identifier) *AtMostConstraint {
	c := &AtMostConstraint{
		MutableConstraintBase: MutableConstraintBase{
			constraintID: constraintID,
			kind:         ConstraintKindAtMost,
			properties:   map[string]interface{}{},
			lock:         sync.RWMutex{},
		},
		ActivationSet: utils.NewActivationSet[deppy.Identifier](),
		n:             n,
	}
	for _, variable := range variables {
		c.Activate(variable)
	}
	return c
}

func (constraint *AtMostConstraint) Merge(other deppy.Constraint) (bool, error) {
	changed := false
	if cc, ok := other.(*AtMostConstraint); ok {
		if constraint.ActivationSet == nil {
			constraint.ActivationSet = utils.NewActivationSet[deppy.Identifier]()
		}
		ok, err := constraint.ActivationSet.Merge(cc.ActivationSet)
		if err != nil {
			return false, err
		}
		changed = changed || ok
	} else if !ok {
		return false, deppy.ConflictErrorf("cannot merge constraints of different kind [%T != %T]", constraint, other)
	}
	if constraint.n != -1 && other.(*AtMostConstraint).n != -1 && constraint.n != other.(*AtMostConstraint).n {
		return false, deppy.ConflictErrorf("cannot merge constraints with different n [%d != %d]", constraint.n, other.(*AtMostConstraint).n)
	}
	if constraint.n == -1 {
		constraint.n = other.(*AtMostConstraint).n
		changed = true
	}
	ok, err := constraint.MutableConstraintBase.Merge(other)
	if err != nil {
		return false, err
	}
	return changed || ok, nil
}

func (constraint *AtMostConstraint) Apply(lm deppy.LitMapping, _ deppy.Identifier) z.Lit {
	ids := constraint.Elements()
	ms := make([]z.Lit, len(ids))
	for i, each := range ids {
		ms[i] = lm.LitOf(each)
	}
	return lm.LogicCircuit().CardSort(ms).Leq(constraint.n)
}

func (constraint *AtMostConstraint) Order() []deppy.Identifier {
	return constraint.Elements()
}

func (constraint *AtMostConstraint) Anchor() bool {
	return false
}

func (constraint *AtMostConstraint) String(subject deppy.Identifier) string {
	ids := constraint.Elements()
	s := make([]string, len(ids))
	for i, each := range ids {
		s[i] = string(each)
	}
	return fmt.Sprintf("%s permits at most %d of %s", subject, constraint.n, strings.Join(s, ", "))
}

func (constraint *AtMostConstraint) SetN(n int) error {
	constraint.lock.Lock()
	defer constraint.lock.Unlock()
	if n < 0 {
		return deppy.FatalError("n must be greater than or equal to 0")
	}
	if constraint.n > 0 {
		return deppy.FatalError(fmt.Sprintf("n is already set to %d", constraint.n))
	}
	constraint.n = n
	return nil
}

func (constraint *AtMostConstraint) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind       string                                 `json:"kind"`
		Properties map[string]interface{}                 `json:"properties"`
		Variables  *utils.ActivationSet[deppy.Identifier] `json:"variables"`
		N          int                                    `json:"n"`
	}{
		Kind:       constraint.Kind(),
		Properties: constraint.GetProperties(),
		Variables:  constraint.ActivationSet,
		N:          constraint.n,
	})
}

func (constraint *AtMostConstraint) UnmarshalJSON(jsonBytes []byte) error {
	data := &struct {
		Kind       string                                 `json:"kind"`
		Properties map[string]interface{}                 `json:"properties"`
		Variables  *utils.ActivationSet[deppy.Identifier] `json:"variables"`
		N          int                                    `json:"n"`
	}{}
	if err := json.Unmarshal(jsonBytes, data); err != nil {
		return err
	}
	constraint.kind = data.Kind
	constraint.properties = data.Properties
	constraint.ActivationSet = data.Variables
	constraint.n = data.N
	return nil
}
