//go:build !ignore_autogenerated

/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BundleMetadata) DeepCopyInto(out *BundleMetadata) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BundleMetadata.
func (in *BundleMetadata) DeepCopy() *BundleMetadata {
	if in == nil {
		return nil
	}
	out := new(BundleMetadata)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CRDUpgradeSafetyPreflightConfig) DeepCopyInto(out *CRDUpgradeSafetyPreflightConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CRDUpgradeSafetyPreflightConfig.
func (in *CRDUpgradeSafetyPreflightConfig) DeepCopy() *CRDUpgradeSafetyPreflightConfig {
	if in == nil {
		return nil
	}
	out := new(CRDUpgradeSafetyPreflightConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CatalogSource) DeepCopyInto(out *CatalogSource) {
	*out = *in
	if in.Channels != nil {
		in, out := &in.Channels, &out.Channels
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CatalogSource.
func (in *CatalogSource) DeepCopy() *CatalogSource {
	if in == nil {
		return nil
	}
	out := new(CatalogSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterExtension) DeepCopyInto(out *ClusterExtension) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterExtension.
func (in *ClusterExtension) DeepCopy() *ClusterExtension {
	if in == nil {
		return nil
	}
	out := new(ClusterExtension)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterExtension) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterExtensionInstallConfig) DeepCopyInto(out *ClusterExtensionInstallConfig) {
	*out = *in
	if in.Preflight != nil {
		in, out := &in.Preflight, &out.Preflight
		*out = new(PreflightConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterExtensionInstallConfig.
func (in *ClusterExtensionInstallConfig) DeepCopy() *ClusterExtensionInstallConfig {
	if in == nil {
		return nil
	}
	out := new(ClusterExtensionInstallConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterExtensionInstallStatus) DeepCopyInto(out *ClusterExtensionInstallStatus) {
	*out = *in
	out.Bundle = in.Bundle
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterExtensionInstallStatus.
func (in *ClusterExtensionInstallStatus) DeepCopy() *ClusterExtensionInstallStatus {
	if in == nil {
		return nil
	}
	out := new(ClusterExtensionInstallStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterExtensionList) DeepCopyInto(out *ClusterExtensionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ClusterExtension, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterExtensionList.
func (in *ClusterExtensionList) DeepCopy() *ClusterExtensionList {
	if in == nil {
		return nil
	}
	out := new(ClusterExtensionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClusterExtensionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterExtensionSpec) DeepCopyInto(out *ClusterExtensionSpec) {
	*out = *in
	out.ServiceAccount = in.ServiceAccount
	in.Source.DeepCopyInto(&out.Source)
	if in.Install != nil {
		in, out := &in.Install, &out.Install
		*out = new(ClusterExtensionInstallConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterExtensionSpec.
func (in *ClusterExtensionSpec) DeepCopy() *ClusterExtensionSpec {
	if in == nil {
		return nil
	}
	out := new(ClusterExtensionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterExtensionStatus) DeepCopyInto(out *ClusterExtensionStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Install != nil {
		in, out := &in.Install, &out.Install
		*out = new(ClusterExtensionInstallStatus)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterExtensionStatus.
func (in *ClusterExtensionStatus) DeepCopy() *ClusterExtensionStatus {
	if in == nil {
		return nil
	}
	out := new(ClusterExtensionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreflightConfig) DeepCopyInto(out *PreflightConfig) {
	*out = *in
	if in.CRDUpgradeSafety != nil {
		in, out := &in.CRDUpgradeSafety, &out.CRDUpgradeSafety
		*out = new(CRDUpgradeSafetyPreflightConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreflightConfig.
func (in *PreflightConfig) DeepCopy() *PreflightConfig {
	if in == nil {
		return nil
	}
	out := new(PreflightConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceAccountReference) DeepCopyInto(out *ServiceAccountReference) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceAccountReference.
func (in *ServiceAccountReference) DeepCopy() *ServiceAccountReference {
	if in == nil {
		return nil
	}
	out := new(ServiceAccountReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SourceConfig) DeepCopyInto(out *SourceConfig) {
	*out = *in
	if in.Catalog != nil {
		in, out := &in.Catalog, &out.Catalog
		*out = new(CatalogSource)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SourceConfig.
func (in *SourceConfig) DeepCopy() *SourceConfig {
	if in == nil {
		return nil
	}
	out := new(SourceConfig)
	in.DeepCopyInto(out)
	return out
}
