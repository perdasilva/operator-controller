package maps_test

import (
	"github.com/operator-framework/operator-controller/internal/util/maps"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMergeMaps(t *testing.T) {
	tests := []struct {
		name      string
		maps      []map[string]string
		expectMap map[string]string
	}{
		{
			name:      "no maps",
			maps:      make([]map[string]string, 0),
			expectMap: map[string]string{},
		},
		{
			name:      "two empty maps",
			maps:      []map[string]string{{}, {}},
			expectMap: map[string]string{},
		},
		{
			name:      "simple add",
			maps:      []map[string]string{{"foo": "bar"}, {"bar": "foo"}},
			expectMap: map[string]string{"foo": "bar", "bar": "foo"},
		},
		{
			name:      "subsequent maps overwrite prior",
			maps:      []map[string]string{{"foo": "bar", "bar": "foo"}, {"foo": "foo"}, {"bar": "bar"}},
			expectMap: map[string]string{"foo": "foo", "bar": "bar"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equalf(t, tc.expectMap, maps.MergeMaps(tc.maps...), "maps did not merge as expected")
		})
	}
}
