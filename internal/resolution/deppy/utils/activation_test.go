package utils_test

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"strings"
	"testing"

	. "github.com/perdasilva/replee/pkg/deppy/utils"
)

func TestActivationVariable(t *testing.T) {
	tt := []struct {
		name               string
		description        string
		value              string
		action             func(*ActivationValue[string])
		expectedActivation bool
	}{
		{
			name:               "creation",
			description:        "should create a new activated activation variable",
			value:              "foo",
			expectedActivation: true,
		}, {
			name:               "activation",
			description:        "should activate an activation variable",
			value:              "foo",
			action:             func(v *ActivationValue[string]) { v.Activate() },
			expectedActivation: true,
		}, {
			name:               "deactivation",
			description:        "should deactivate an activation variable",
			value:              "foo",
			action:             func(v *ActivationValue[string]) { v.Deactivate() },
			expectedActivation: false,
		}, {
			name:        "commutative",
			description: "action order should not matter",
			value:       "foo",
			action: func(v *ActivationValue[string]) {
				// 3 x activate, 4 x deactivate
				action := []func(){
					v.Activate,
					v.Deactivate,
					v.Activate,
					v.Deactivate,
					v.Activate,
					v.Deactivate,
					v.Deactivate,
				}

				rand.Shuffle(len(action), func(i, j int) {
					action[i], action[j] = action[j], action[i]
				})

				for _, a := range action {
					a()
				}
			},
			expectedActivation: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			v := NewActivationVariable(tc.value)
			if tc.action != nil {
				tc.action(v)
			}
			assert.Equal(t, tc.expectedActivation, v.IsActivated())
			assert.Equal(t, tc.value, v.Value())
		})
	}
}

func TestActivationVariableJSONMarshal(t *testing.T) {
	tt := []struct {
		name         string
		description  string
		value        string
		activate     bool
		expectedJSON string
	}{
		{
			name:         "activated",
			description:  "should marshal an activated activation variable",
			value:        "foo",
			activate:     true,
			expectedJSON: `{"value":"foo","activated":true}`,
		}, {
			name:         "deactivated",
			description:  "should marshal a deactivated activation variable",
			value:        "foo",
			activate:     false,
			expectedJSON: `{"value":"foo","activated":false}`,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			v := NewActivationVariable(tc.value)
			if !tc.activate {
				v.Deactivate()
			}
			jsonBytes, err := v.MarshalJSON()
			assert.NoError(t, err)
			assert.NotNil(t, jsonBytes)
			assert.Equal(t, tc.expectedJSON, string(jsonBytes))
		})
	}
}

func TestActivationVariableJSONUnmarshal(t *testing.T) {
	tt := []struct {
		name               string
		description        string
		json               string
		expectedValue      string
		expectedActivation bool
	}{
		{
			name:               "activated",
			description:        "should unmarshal an activated activation variable",
			json:               "{\"value\":\"foo\",\"activated\":true}",
			expectedValue:      "foo",
			expectedActivation: true,
		}, {
			name:               "deactivated",
			description:        "should unmarshal a deactivated activation variable",
			json:               "{\"value\":\"foo\",\"activated\":false}",
			expectedValue:      "foo",
			expectedActivation: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			v := NewActivationVariable("")
			err := json.Unmarshal([]byte(tc.json), v)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedValue, v.Value())
			assert.Equal(t, tc.expectedActivation, v.IsActivated())
		})
	}
}

func TestActivationSet(t *testing.T) {
	tt := []struct {
		name               string
		description        string
		values             []string
		action             func(*ActivationSet[string])
		expectedActivation []bool
	}{
		{
			name:               "creation",
			description:        "should create a new activation set with all values activated",
			values:             []string{"foo", "bar", "baz"},
			expectedActivation: []bool{true, true, true},
		}, {
			name:               "deactivation",
			description:        "should deactivate a value",
			values:             []string{"foo", "bar", "baz"},
			action:             func(aSet *ActivationSet[string]) { aSet.Deactivate("bar") },
			expectedActivation: []bool{true, false, true},
		}, {
			name:        "commutative",
			description: "order of operator should not matter to the final activation state",
			values:      []string{"foo", "bar", "baz"},
			action: func(aSet *ActivationSet[string]) {
				// 4 x activate, 5 x deactivate
				action := []func(keys ...string){
					aSet.Activate,
					aSet.Deactivate,
					aSet.Activate,
					aSet.Deactivate,
					aSet.Activate,
					aSet.Deactivate,
					aSet.Deactivate,
					aSet.Activate,
					aSet.Deactivate,
				}

				rand.Shuffle(len(action), func(i, j int) {
					action[i], action[j] = action[j], action[i]
				})

				for _, a := range action {
					a("bar")
				}
			},
			expectedActivation: []bool{true, false, true},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, len(tc.values), len(tc.expectedActivation))

			aSet := NewActivationSet(tc.values...)
			if tc.action != nil {
				tc.action(aSet)
			}

			for i := 0; i < len(tc.values); i++ {
				activated, err := aSet.IsActivated(tc.values[i])
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedActivation[i], activated)
			}
		})
	}
}

func TestActivationSetJSONMarshal(t *testing.T) {
	tt := []struct {
		name         string
		description  string
		values       []string
		activations  []bool
		expectedJSON string
	}{
		{
			name:         "json marshal",
			description:  "should marshal activation set to json",
			values:       []string{"foo", "bar", "baz"},
			activations:  []bool{true, false, true},
			expectedJSON: `{"foo":true,"bar":false,"baz":true}`,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			aSet := NewActivationSet(tc.values...)
			for i := 0; i < len(tc.values); i++ {
				if !tc.activations[i] {
					aSet.Deactivate(tc.values[i])
				}
			}

			jsonBytes, err := aSet.MarshalJSON()
			assert.NoError(t, err)
			assert.NotNil(t, jsonBytes)
			tokenize := func(s string) []string {
				return strings.Split(strings.Trim(s, "{}"), ",")
			}
			assert.ElementsMatch(t, tokenize(tc.expectedJSON), tokenize(string(jsonBytes)))
		})
	}
}

func TestActivationSetJSONUnmarshal(t *testing.T) {
	tt := []struct {
		name                string
		description         string
		json                string
		expectedValues      []string
		expectedActivations []bool
	}{
		{
			name:                "json unmarshal",
			description:         "should unmarshal json to activation set",
			json:                `{"foo":true,"bar":false,"baz":true}`,
			expectedValues:      []string{"foo", "bar", "baz"},
			expectedActivations: []bool{true, false, true},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			v := NewActivationSet[string]()
			err := json.Unmarshal([]byte(tc.json), &v)
			assert.NoError(t, err)
			for i := 0; i < len(tc.expectedValues); i++ {
				activated, err := v.IsActivated(tc.expectedValues[i])
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedActivations[i], activated)
			}
		})
	}
}

func TestActivationMap(t *testing.T) {
	tt := []struct {
		name               string
		description        string
		key                int
		value              string
		action             func(*ActivationMap[int, string])
		expectedActivation bool
	}{
		{
			name:               "creation",
			description:        "should create a new activation map with a value activated",
			key:                1,
			value:              "one",
			expectedActivation: true,
		}, {
			name:               "deactivation",
			description:        "should deactivate a value",
			key:                1,
			value:              "one",
			action:             func(m *ActivationMap[int, string]) { m.Deactivate(1) },
			expectedActivation: false,
		}, {
			name:        "commutative",
			description: "order of operator should not matter to the final activation state",
			key:         1,
			value:       "one",
			action: func(aMap *ActivationMap[int, string]) {
				// 4 x activate, 5 x deactivate
				action := []func(key int){
					aMap.Activate,
					aMap.Deactivate,
					aMap.Activate,
					aMap.Deactivate,
					aMap.Activate,
					aMap.Deactivate,
					aMap.Deactivate,
					aMap.Activate,
					aMap.Deactivate,
				}

				rand.Shuffle(len(action), func(i, j int) {
					action[i], action[j] = action[j], action[i]
				})

				for _, a := range action {
					a(1)
				}
			},
			expectedActivation: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			m := NewActivationMap[int, string]()
			m.Put(tc.key, tc.value)
			if tc.action != nil {
				tc.action(m)
			}
			activated, err := m.IsActivated(tc.key)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedActivation, activated)
		})
	}
}

func TestActivationMapJSONMarshal(t *testing.T) {
	tt := []struct {
		name         string
		description  string
		key          int
		value        string
		activate     bool
		expectedJSON string
	}{
		{
			name:         "activated",
			description:  "should marshal an activated activation map",
			key:          1,
			value:        "one",
			activate:     true,
			expectedJSON: `{"1":{"value":"one","activated":true}}`,
		},
		{
			name:         "deactivated",
			description:  "should marshal a deactivated activation map",
			key:          1,
			value:        "one",
			activate:     false,
			expectedJSON: `{"1":{"value":"one","activated":false}}`,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			m := NewActivationMap[int, string]()
			m.Put(tc.key, tc.value)
			if tc.activate {
				m.Activate(tc.key)
			} else {
				m.Deactivate(tc.key)
			}
			jsonBytes, err := m.MarshalJSON()
			assert.NoError(t, err)
			assert.NotNil(t, jsonBytes)
			assert.Equal(t, tc.expectedJSON, string(jsonBytes))
		})
	}
}

func TestActivationMapJSONUnmarshal(t *testing.T) {
	tt := []struct {
		name               string
		description        string
		json               string
		expectedKey        int
		expectedValue      string
		expectedActivation bool
	}{
		{
			name:               "activated",
			description:        "should unmarshal an activated activation map",
			json:               `{"1":{"value":"one","activated":true}}`,
			expectedKey:        1,
			expectedValue:      "one",
			expectedActivation: true,
		},
		{
			name:               "deactivated",
			description:        "should unmarshal a deactivated activation map",
			json:               `{"1":{"value":"one","activated":false}}`,
			expectedKey:        1,
			expectedValue:      "one",
			expectedActivation: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			m := NewActivationMap[int, string]()
			err := json.Unmarshal([]byte(tc.json), &m)
			assert.NoError(t, err)
			activated, err := m.IsActivated(tc.expectedKey)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedActivation, activated)
			value, ok := m.GetValue(tc.expectedKey)
			assert.True(t, ok)
			assert.Equal(t, tc.expectedValue, value)
		})
	}
}
