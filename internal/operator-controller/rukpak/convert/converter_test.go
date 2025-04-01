package convert_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
)

func TestConverterValidatesBundle(t *testing.T) {
	converter := convert.Converter{
		BundleValidator: []func(rv1 *convert.RegistryV1) []error{
			func(rv1 *convert.RegistryV1) []error {
				return []error{errors.New("test error")}
			},
		},
	}

	_, err := converter.Convert(convert.RegistryV1{}, "installNamespace", []string{"watchNamespace"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "test error")
}
