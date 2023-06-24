package resolution

import (
	"encoding/json"
	"fmt"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/utils"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/variables"
)

var _ deppy.MutableResolutionProblem = &MutableResolutionProblem{}

type MutableResolutionProblem struct {
	resolutionProblemID deppy.Identifier
	variables           *utils.ActivationMap[deppy.Identifier, deppy.MutableVariable]
}

func NewMutableResolutionProblem(resolutionProblemID deppy.Identifier) *MutableResolutionProblem {
	return &MutableResolutionProblem{
		resolutionProblemID: resolutionProblemID,
		variables:           utils.NewActivationMap[deppy.Identifier, deppy.MutableVariable](),
	}
}

func (m *MutableResolutionProblem) String() string {
	str, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Sprintf("error marshaling resolution problem: %s", err)
	}
	return string(str)
}

func (m *MutableResolutionProblem) MarshalJSON() ([]byte, error) {
	bytes, err := json.Marshal(&struct {
		ResolutionProblemID deppy.Identifier                                              `json:"resolutionProblemID"`
		Variables           *utils.ActivationMap[deppy.Identifier, deppy.MutableVariable] `json:"variables"`
	}{
		ResolutionProblemID: m.resolutionProblemID,
		Variables:           m.variables,
	})
	return bytes, err
}

func (m *MutableResolutionProblem) UnmarshalJSON(jsonBytes []byte) error {
	data := &struct {
		ResolutionProblemID deppy.Identifier                                        `json:"resolutionProblemID"`
		Variables           *utils.ActivationMap[deppy.Identifier, json.RawMessage] `json:"variables"`
	}{}
	if err := json.Unmarshal(jsonBytes, data); err != nil {
		return err
	}
	m.resolutionProblemID = data.ResolutionProblemID
	m.variables = utils.NewActivationMap[deppy.Identifier, deppy.MutableVariable]()
	varIDs := data.Variables.Keys()
	for i := 0; i < len(varIDs); i++ {
		variableID := varIDs[i]
		variable := variables.NewMutableVariable(variableID, "", nil)
		variableJSON, _ := data.Variables.GetValue(variableID)
		if err := json.Unmarshal(variableJSON, variable); err != nil {
			return err
		}
		m.variables.Put(variableID, variable)
		if activated, err := data.Variables.IsActivated(variableID); err != nil {
			return err
		} else if !activated {
			m.variables.Deactivate(variableID)
		}
	}
	return nil
}

func (m *MutableResolutionProblem) GetVariable(variableID deppy.Identifier, kind string) (deppy.Variable, error) {
	if v, ok := m.variables.GetValue(variableID); ok {
		if v.Kind() != kind {
			return nil, deppy.ConflictErrorf("variable %s is not of kind %s", variableID, kind)
		}
		return v, nil
	} else {
		v := variables.NewMutableVariable(variableID, kind, nil)
		m.variables.Put(variableID, v)
		return v, nil
	}
}

func (m *MutableResolutionProblem) GetMutableVariable(variableID deppy.Identifier, kind string) (deppy.MutableVariable, error) {
	if v, ok := m.variables.GetValue(variableID); ok {
		if v.Kind() != kind {
			return nil, deppy.ConflictErrorf("variable %s is not of kind %s", variableID, kind)
		}
		return v, nil
	} else {
		v := variables.NewMutableVariable(variableID, kind, nil)
		m.variables.Put(variableID, v)
		return v, nil
	}
}

func (m *MutableResolutionProblem) ActivateVariable(v deppy.MutableVariable) error {
	if vr, ok := m.variables.GetValue(v.Identifier()); ok {
		if vr.Kind() != v.Kind() {
			return fmt.Errorf("variable %s is not of kind %s", v.Identifier(), v.Kind())
		}
		_, err := vr.Merge(v)
		if err != nil {
			return err
		}
	} else {
		m.variables.Put(v.Identifier(), v)
	}
	if activated, err := m.variables.IsActivated(v.Identifier()); err != nil {
		return err
	} else if !activated {
		m.variables.Deactivate(v.Identifier())
	}
	return nil
}

func (m *MutableResolutionProblem) DeactivateVariable(variableID deppy.Identifier, kind string) error {
	if v, ok := m.variables.GetValue(variableID); ok {
		if v.Kind() != kind {
			return fmt.Errorf("variable %s is not of kind %s", variableID, kind)
		}
	} else {
		v := variables.NewMutableVariable(variableID, kind, nil)
		m.variables.Put(variableID, v)
	}
	m.variables.Deactivate(variableID)
	return nil
}

func (m *MutableResolutionProblem) ResolutionProblemID() deppy.Identifier {
	return m.resolutionProblemID
}

func (m *MutableResolutionProblem) GetMutableVariables() ([]deppy.MutableVariable, error) {
	return m.variables.Elements(), nil
}

func (m *MutableResolutionProblem) GetVariables() ([]deppy.Variable, error) {
	var vars []deppy.Variable
	for _, v := range m.variables.Elements() {
		vars = append(vars, v)
	}
	return vars, nil
}

func (m *MutableResolutionProblem) Options() []deppy.ResolutionOption {
	return nil
}
