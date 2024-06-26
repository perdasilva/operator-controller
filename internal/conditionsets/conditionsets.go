/*
Copyright 2023.

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

package conditionsets

// ConditionTypes is the full set of ClusterExtension condition Types.
// ConditionReasons is the full set of ClusterExtension condition Reasons.
//
// NOTE: These are populated by init() in api/v1alpha1/clusterextension_types.go
var ConditionTypes []string
var ConditionReasons []string

// ExtensionConditionTypes is the set of Extension exclusive condition Types.
var ExtensionConditionTypes []string
var ExtensionConditionReasons []string
