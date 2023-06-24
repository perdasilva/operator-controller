package resolution_test

import (
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/resolution"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/variables"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMutableResolutionProblem_MarshalJSON(t *testing.T) {
	tt := []struct {
		name         string
		description  string
		problem      *resolution.MutableResolutionProblem
		expectedJSON string
	}{
		{
			name:        "marshal a resolution problem with a single variable",
			description: "should marshal an activated activation variable",
			problem: func(t *testing.T) *resolution.MutableResolutionProblem {
				m := resolution.NewMutableResolutionProblem("foo")
				err := m.ActivateVariable(variables.NewMutableVariable("foo", "deppy.var.test", map[string]interface{}{
					"key": "value",
					"prop": map[string]interface{}{
						"key": "value",
					},
				}))
				assert.NoError(t, err)
				return m
			}(t),
			expectedJSON: `{"resolutionProblemID":"foo","variables":{"foo":{"value":{"variableID":"foo","kind":"deppy.var.test","properties":{"key":"value","prop":{"key":"value"}},"constraints":{}},"activated":true}}}`,
		}, {
			name:        "marshal a resolution problem with a single variable and a single constraint",
			description: "should marshal an activated activation variable",
			problem: func(t *testing.T) *resolution.MutableResolutionProblem {
				m := resolution.NewMutableResolutionProblem("foo")
				v := variables.NewMutableVariable("foo", "deppy.var.test", map[string]interface{}{
					"key": "value",
					"prop": map[string]interface{}{
						"key": "value",
					},
				})

				err := v.AddDependency("cId", "v1", "v2", "v3")
				assert.NoError(t, err)

				err = m.ActivateVariable(v)
				assert.NoError(t, err)
				return m
			}(t),
			expectedJSON: `{"resolutionProblemID":"foo","variables":{"foo":{"value":{"variableID":"foo","kind":"deppy.var.test","properties":{"key":"value","prop":{"key":"value"}},"constraints":{"cId":{"value":{"kind":"deppy.constraint.dependency","properties":{},"dependencyIDs":{"v1":true,"v2":true,"v3":true}},"activated":true}}},"activated":true}}}`,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := tc.problem.MarshalJSON()
			assert.NoError(t, err)
			assert.NotNil(t, jsonBytes)
			assert.Equal(t, tc.expectedJSON, string(jsonBytes))
		})
	}
}

//func TestActivationVariableJSONUnmarshal(t *testing.T) {
//	tt := []struct {
//		name               string
//		description        string
//		json               string
//		expectedValue      string
//		expectedActivation bool
//	}{
//		{
//			name:               "activated",
//			description:        "should unmarshal an activated activation variable",
//			json:               "{"value":"foo","activated":true}",
//			expectedValue:      "foo",
//			expectedActivation: true,
//		}, {
//			name:               "deactivated",
//			description:        "should unmarshal a deactivated activation variable",
//			json:               "{"value":"foo","activated":false}",
//			expectedValue:      "foo",
//			expectedActivation: false,
//		},
//	}
//
//	for _, tc := range tt {
//		t.Run(tc.name, func(t *testing.T) {
//			v := NewActivationVariable("")
//			err := json.Unmarshal([]byte(tc.json), v)
//			assert.NoError(t, err)
//			assert.Equal(t, tc.expectedValue, v.Value())
//			assert.Equal(t, tc.expectedActivation, v.IsActivated())
//		})
//	}
//}
