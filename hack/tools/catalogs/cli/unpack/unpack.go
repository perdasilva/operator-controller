package bundle

import (
	"context"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/pkg/docker/config"
	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"github.com/operator-framework/operator-controller/internal/rukpak/source"
	fsutil "github.com/operator-framework/operator-controller/internal/util/fs"
	"io/fs"
	"oras.land/oras-go/v2/registry/remote/auth"
	"strings"
)

func Unpack(ctx context.Context, unpackPath string, imageRef string) (fs.FS, error) {
	ref, err := reference.ParseNamed(imageRef)
	if err != nil {
		return nil, err
	}

	unpacker := source.ContainersImageRegistry{
		BaseCachePath: unpackPath,
		SourceContextFunc: func(logger logr.Logger) (*types.SystemContext, error) {
			authConfig, err := config.GetCredentialsForRef(nil, ref)
			if err != nil {
				return nil, err
			}
			return &types.SystemContext{
				DockerAuthConfig: &authConfig,
			}, nil
		},
	}

	result, err := unpacker.Unpack(ctx, &source.BundleSource{
		Name: getImageName(ref),
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			Ref: imageRef,
		},
	})

	if err != nil {
		return nil, err
	}

	if err := fsutil.SetWritableRecursive(unpackPath); err != nil {
		return nil, err
	}

	return result.Bundle, nil
}

func getImageName(ref reference.Named) string {
	path := reference.Path(ref)
	tokens := strings.Split(path, "/")
	return tokens[len(tokens)-1]
}

func getCredentials(repoName string) func(context.Context, string) (auth.Credential, error) {
	return func(ctx context.Context, _ string) (auth.Credential, error) {
		ref, err := reference.ParseNamed(repoName)
		if err != nil {
			return auth.Credential{}, err
		}
		authConfig, err := config.GetCredentialsForRef(nil, ref)
		if err != nil {
			return auth.Credential{}, err
		}
		return auth.Credential{
			Username: authConfig.Username,
			Password: authConfig.Password,
		}, nil
	}
}
