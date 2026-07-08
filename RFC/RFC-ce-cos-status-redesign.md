# **RFC**: ClusterExtension and ClusterObjectSet Status Redesign

## Improving Status Observability: Making CE Self-Sufficient and COS Phase-Aware

**Author Name(s)**: Per G. da Silva
**Author Date**: July 7, 2026
**Feedback Due Date**: **July 21, 2026**
**Status:** Draft
**Approval:** TBD
**Parent Brief:** \<none\>

# **Need**

Users of OLM v1 must currently inspect ClusterObjectSet (COS) resources — an implementation detail — to understand what is happening with their ClusterExtension (CE) deployments. The CE status surface has several issues that make it insufficient for day-to-day operations:

1. **Confusing condition semantics**: `Progressing=True, Reason=Succeeded` means "finished progressing," which contradicts the natural reading of `Progressing=True` as "active work is happening." This violates the [Kubernetes API conventions for conditions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties).

2. **Mirrored COS conditions on CE**: The CE controller mirrors COS `Available` and `Progressing` conditions directly onto the CE, mixing COS-specific reasons (like `ProbesSucceeded`, `RollingOut`) with CE-native reasons. This couples CE's API surface to COS internals.

3. **No health signal**: The CE has no dedicated "is my extension healthy right now?" condition. `Installed=True` means "a bundle was installed," not "the managed resources are currently healthy."

4. **No upgrade visibility**: During an upgrade, users cannot see what version is being rolled out to from the CE status alone. The target version is only visible in COS annotations.

5. **No phase visibility on COS**: When a COS rollout stalls, users get a `ProbeFailure` message but cannot see which phase is stuck, which phases completed, or the overall progress through the phased rollout.

6. **Print columns don't surface what matters**: The CE print columns show `Installed Bundle` (rarely needed at a glance) and the `Installed` condition (less actionable than a health signal).

# **Approach**

## Overview

This RFC proposes changes across both the CE and COS APIs to make the CE self-sufficient for user observability while adding phase-level detail to COS for advanced debugging.

**Guiding principle**: COS is an implementation detail. Users should be able to understand, diagnose, and act on their CE status without ever looking at a COS. COS inspection is reserved for extreme debugging scenarios.

## Part 1: ClusterExtension Status Changes

### 1.1 Fix Progressing Condition Semantics (Bug Fix)

**Current (broken)**:
- `Progressing=True, Reason=Succeeded` → "finished progressing"
- `Progressing=True, Reason=Retrying` → "retrying after error"
- `Progressing=True, Reason=RollingOut` → "active rollout"
- `Progressing=False, Reason=Blocked` → "terminal error"

**Proposed (fixed)**:
- `Progressing=False, Reason=Succeeded` → "finished, not progressing anymore"
- `Progressing=True, Reason=RollingOut` → "active rollout, no issues" (unchanged)
- `Progressing=True, Reason=ProbeFailure` → "rollout in progress but probes failing"
- `Progressing=True, Reason=ResolutionFailed` → "bundle resolution failed, retrying"
- `Progressing=True, Reason=ImagePullFailed` → "image pull failed, retrying"
- `Progressing=True, Reason=ValidationFailed` → "CE validation failed (e.g., ServiceAccount not found), retrying"
- `Progressing=True, Reason=AuthorizationFailed` → "RBAC insufficient, retrying"
- `Progressing=True, Reason=UnsupportedContent` → "bundle content unsupported, retrying"
- `Progressing=True, Reason=PreflightFailed` → "CRD safety or other preflight check failed, retrying"
- `Progressing=True, Reason=Retrying` → "COS-level transient error, retrying"
- `Progressing=False, Reason=Blocked` → "terminal error" (unchanged)
- `Progressing=False, Reason=InvalidConfiguration` → "invalid configuration, requires manual fix"
- `Progressing=False, Reason=ProgressDeadlineExceeded` → "timed out"

This aligns with the Kubernetes convention: `Progressing=True` means active work is happening. This change can be treated as a bug fix since the current behavior contradicts the documented convention.

### 1.2 Add Ready Condition

A new `Ready` condition provides a dedicated health signal for the extension's managed resources. "Ready" is chosen over "Available" because "Available" implies service delivery semantics that OLM cannot guarantee — OLM can confirm that managed resources are on-cluster and passing probes, but not that the application is serving traffic correctly.

| Status | Reason | Meaning |
|--------|--------|---------|
| True | `Succeeded` | Resources healthy, all probes pass |
| False | `Absent` | No bundle installed yet (nothing deployed to be healthy) |
| False | `ProbeFailure` | Specific probe failure on managed resources |
| False | `RollingOut` | Rollout in progress, objects in transition — probes not yet passing |
| Unknown | `Pending` | Initial state before first reconcile |

**How Ready is derived**:

Ready answers ONE question: **"are the managed resources currently healthy?"** It does not carry pipeline error detail (resolution failures, config errors, pull errors) — that is `Progressing`'s job. This follows the Kubernetes Deployment pattern where `Available` and `Progressing` are orthogonal signals that don't duplicate each other's information.

- **When a COS revision is rolling out** (latest active COS without `succeededAt`): `Ready` reflects that COS's probe state. If it's still rolling out and probes haven't been evaluated, `Ready=False/RollingOut`. If probes are failing, `Ready=False/ProbeFailure`. This means **Ready drops during normal upgrades** — it tracks the actual on-cluster state, not a cached view from the old revision.
- **When only the installed revision exists** (no rollout in progress): `Ready` reflects the installed COS's probe state. If probes pass, `Ready=True/Succeeded`. If probes fail, `Ready=False/ProbeFailure`.
- **When a pre-COS error occurs** (resolution failure, pull failure, validation error — no new COS is created): If an installed revision exists, `Ready` stays based on that installed revision's probe state (the error didn't create a new COS, so the on-cluster state hasn't changed). If no installed revision exists, `Ready=False/Absent` — there's simply nothing deployed. The specific error goes in `Progressing`, not `Ready`.
- **When nothing is installed and no error has occurred yet**: `Ready=Unknown/Pending` (brief initial state before first reconcile).

**Why Ready drops during upgrades**: During a multi-revision transition, objects are adopted by the new COS phase by phase. The old COS reports handed-off objects as `Progressed` (complete) even though the new COS's version of those objects might be failing probes. Deriving Ready from the old COS would be misleading — it would say "healthy" when the actual on-cluster objects are in a mixed or broken state. By tracking the latest active COS's probe state, Ready reflects what is actually deployed.

**Ready vs Installed**: These answer different questions:
- `Installed` → "Has a bundle been successfully installed?" (static fact, based on whether any COS has completed rollout)
- `Ready` → "Are the managed resources currently healthy right now?" (dynamic health check, tracks the latest active COS's probe state)

You can have `Installed=True, Ready=False/RollingOut` during a normal upgrade — the old version was installed but objects are being replaced, and the new revision hasn't completed. Once the upgrade finishes, Ready returns to True.

### 1.3 Add Operation Status Field

A new `status.operation` field provides structured information about an active rollout, including the target bundle version and the type of rollout.

```go
type ClusterExtensionOperationStatus struct {
    // type indicates the kind of rollout in progress.
    // "Install" for first-time installations, "Upgrade" for version changes,
    // "Reconfigure" for same-version configuration changes.
    // +required
    // +kubebuilder:validation:Enum=Install;Upgrade;Reconfigure
    Type OperationType `json:"type"`

    // bundle identifies the target bundle being rolled out.
    // +required
    Bundle BundleMetadata `json:"bundle"`
}

type OperationType string

const (
    OperationTypeInstall     OperationType = "Install"
    OperationTypeUpgrade     OperationType = "Upgrade"
    OperationTypeReconfigure OperationType = "Reconfigure"
)
```

**When populated**: `status.operation` is non-nil whenever a rollout has been attempted and has not yet succeeded. This includes active rollouts (`Progressing=True`) AND failed/blocked rollouts (`Progressing=False/Blocked`, `Progressing=False/InvalidConfiguration`, `Progressing=False/ProgressDeadlineExceeded`). The user needs to know what they were trying to roll out to, even when it failed.

**When cleared**: `status.operation` is set to nil only when `Progressing=False/Succeeded` — the rollout completed successfully. At that point, `status.install` reflects the new version and `operation` is no longer needed.

**When nil (no rollout information)**: `status.operation` is nil in two cases: (1) the extension is in steady state (`Progressing=False/Succeeded`), and (2) errors that occur *before* bundle resolution completes (e.g., bundle not found, catalog unavailable). In case (2), the controller doesn't yet know the target bundle, so it cannot populate the rollout field. The `Progressing` condition message carries the error context instead.

**How the type is determined by the controller**:
- `Install`: `status.install` is nil (no previous installation)
- `Upgrade`: `status.install` is non-nil and the target bundle version differs from the installed version
- `Reconfigure`: `status.install` is non-nil and the target bundle version matches the installed version

### 1.4 Remove activeRevisions (Experimental)

The `status.activeRevisions` field is experimental and exposes COS implementation details to CE users. With the addition of `Ready` (health signal) and `status.operation` (upgrade visibility), users no longer need to inspect revision-level detail on the CE.

This field is removed. Users who need revision-level detail can inspect COS resources directly.

### 1.5 Stop Mirroring COS Conditions — Translate Instead

The CE controller currently mirrors COS `Available` and `Progressing` conditions directly onto the CE, including COS-specific reasons like `ProbesSucceeded` and `RollingOut`.

**Proposed**: The CE controller reads COS state but translates it into CE-native conditions (`Ready`, `Progressing`, `Installed`) with CE-native reasons. No COS conditions appear on the CE.

**Critical: CE must surface COS blocking errors.** When the latest rolling-out COS has a terminal error (`Progressing=False/Blocked`, `Progressing=False/ProgressDeadlineExceeded`), the CE controller must detect this and reflect it in the CE's `Progressing` condition. Without this, the CE would show `Progressing=True/RollingOut` while the COS is terminally stuck — leaving the user unable to diagnose the problem from the CE alone.

Translation rules for the CE `Progressing` condition from COS state:
- COS `Progressing=True/RollingOut` AND COS `Ready=False/ProbeFailure` → CE `Progressing=True/ProbeFailure` with the probe failure detail. This distinguishes a stuck rollout from a healthy one.
- COS `Progressing=True/RollingOut` AND COS `Ready=False/RollingOut` → CE `Progressing=True/RollingOut` (normal progress, no issues).
- COS `Progressing=True/Retrying` → CE `Progressing=True/Retrying` with the COS error message (collisions, validation errors, etc.)
- COS `Progressing=False/Blocked` → CE `Progressing=False/Blocked` with the COS error message
- COS `Progressing=False/ProgressDeadlineExceeded` → CE `Progressing=False/ProgressDeadlineExceeded` with the COS message

This ensures the RFC's guiding principle holds: users can understand, diagnose, and act on their CE status without ever looking at a COS.

### 1.6 Updated CE Print Columns

**Current**: `Installed Bundle`, `Version`, `Installed`, `Progressing`, `Age`

**Proposed**: `Ready`, `Progressing`, `Reason`, `Version`, `Operation`, `Target`, `Age`

| Column | JSONPath | Rationale |
|--------|---------|-----------|
| Ready | `.status.conditions[?(@.type=='Ready')].status` | Primary health signal — the first thing users check |
| Progressing | `.status.conditions[?(@.type=='Progressing')].status` | Is something actively happening? Standard Kubernetes boolean |
| Reason | `.status.conditions[?(@.type=='Progressing')].reason` | **Why** — the Progressing condition's reason. Each reason identifies a specific error category: `Succeeded`, `RollingOut`, `ProbeFailure`, `ResolutionFailed`, `ImagePullFailed`, `ValidationFailed`, `AuthorizationFailed`, `UnsupportedContent`, `PreflightFailed`, `Retrying`, `Blocked`, `InvalidConfiguration`, `ProgressDeadlineExceeded` |
| Version | `.status.install.bundle.version` | What version is installed |
| Operation | `.status.operation.type` | What kind of rollout is in progress (Install/Upgrade/Reconfigure). Empty in steady state |
| Target | `.status.operation.bundle.version` | What version is being rolled out to. Empty in steady state |
| Message | `.status.conditions[?(@.type=='Progressing')].message` | **Wide only** (priority=1, shown with `-o wide`). The Progressing condition's message — gives the specific error detail inline without requiring `kubectl describe` |
| Age | `.metadata.creationTimestamp` | Standard — always last per kubectl convention |

`Installed Bundle` (the bundle name) is dropped because it's rarely needed at a glance — the CE name itself identifies the extension, and the version is more actionable. The `Installed` condition column is replaced by `Ready`, which is a more useful signal. The `Progressing` column keeps the standard Kubernetes boolean, and the `Reason` column adds the *why* — together they let users triage without `kubectl describe`. The `Operation` and `Target` columns provide upgrade visibility. The `Message` column is hidden by default and shown with `-o wide` — it provides the full error detail for SREs who need it without cluttering the default table.

**Triage at a glance**: `Succeeded` = all good. `RollingOut` = normal upgrade, wait. Any `*Failed` reason = specific retryable problem (use `-o wide` for detail). `Retrying` = COS-level transient error. `Blocked/InvalidConfiguration/ProgressDeadlineExceeded` = **needs attention, won't self-resolve**.

Example (default):

```
$ kubectl get clusterextensions
NAME              READY   PROGRESSING   REASON                VERSION   OPERATION   TARGET   AGE
cert-manager      True    False         Succeeded             1.14.0                         30d
my-operator       False   True          RollingOut            1.0.0     Upgrade     2.0.0    5d
broken-operator   False   False         Blocked               <none>    Install     1.0.0    2h
pull-fail         False   True          ImagePullFailed       <none>    Install     1.0.0    5m
no-rbac           True    True          AuthorizationFailed   1.0.0     Upgrade     2.0.0    5d
```

Example (wide — includes MESSAGE):

```
$ kubectl get clusterextensions -o wide
NAME              READY   PROGRESSING   REASON                VERSION   OPERATION   TARGET   MESSAGE                                                                        AGE
cert-manager      True    False         Succeeded             1.14.0                         Desired state reached                                                          30d
broken-operator   False   False         Blocked               <none>    Install     1.0.0    error parsing image reference "!!!invalid": invalid reference format           2h
pull-fail         False   True          ImagePullFailed       <none>    Install     1.0.0    error copying image: authentication required                                   5m
no-rbac           True    True          AuthorizationFailed   1.0.0     Upgrade     2.0.0    pre-authorization failed: SA requires permissions: [create deployments.apps]   5d
```

### 1.7 Complete CE Condition Summary

| Condition | Status=True | Status=False | Status=Unknown |
|-----------|------------|-------------|----------------|
| **Installed** | `Succeeded` — a bundle is installed | `Absent` — no bundle installed | — |
| **Ready** | `Succeeded` — resources healthy, probes pass | `Absent` — no bundle installed (nothing deployed); `ProbeFailure` — specific probe failure on managed resources; `RollingOut` — objects in transition, probes not yet passing | `Pending` — initial state before first reconcile |
| **Progressing** | `RollingOut` — active rollout, no issues; `ProbeFailure` — rollout active, probes failing; `ResolutionFailed` — bundle not found; `ImagePullFailed` — image pull error; `ValidationFailed` — CE validation error; `AuthorizationFailed` — RBAC insufficient; `UnsupportedContent` — bundle content unsupported; `PreflightFailed` — preflight check failed; `Retrying` — COS-level transient error | `Succeeded` — done; `Blocked` — terminal error; `InvalidConfiguration` — bad config; `ProgressDeadlineExceeded` — timed out | — |
| **Deprecated** | `Deprecated` — any deprecation exists | `NotDeprecated` — no deprecation | `DeprecationStatusUnknown` — catalog data unavailable |
| **PackageDeprecated** | `Deprecated` | `NotDeprecated` | `DeprecationStatusUnknown` |
| **ChannelDeprecated** | `Deprecated` | `NotDeprecated` | `DeprecationStatusUnknown` |
| **BundleDeprecated** | `Deprecated` — installed bundle deprecated | `NotDeprecated` | `DeprecationStatusUnknown`; `Absent` — no bundle installed |

**Key decision: Ready tracks actual on-cluster state**. During upgrades and reconfigurations, `Ready` reflects the latest rolling-out COS's probe state — not the old installed revision's state. This means Ready drops to `False` during normal upgrades (the new revision's objects haven't all passed probes yet). This is intentional: during the transition, objects are being replaced phase by phase, and the old COS's view is stale for objects it has handed off. `Installed` remains `True/Succeeded` (a bundle was installed in the past), and users can check `Progressing` to understand what's happening. When a pre-COS error occurs (resolution, config, pull) and no new COS is created, Ready stays based on the installed revision (the on-cluster state hasn't changed).

### 1.8 Complete CE Status Structure

```go
type ClusterExtensionStatus struct {
    // conditions represents the current state of the ClusterExtension.
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // install is a representation of the current installation status for this ClusterExtension.
    // nil if no bundle has been successfully installed.
    // +optional
    Install *ClusterExtensionInstallStatus `json:"install,omitempty"`

    // operation is a representation of an active operation for this ClusterExtension.
    // nil if no operation is in progress.
    // +optional
    Operation *ClusterExtensionOperationStatus `json:"operation,omitempty"`
}

type ClusterExtensionInstallStatus struct {
    // bundle is required and represents the identifying attributes of the installed bundle.
    // +required
    Bundle BundleMetadata `json:"bundle"`
}

type ClusterExtensionOperationStatus struct {
    // type indicates the kind of rollout in progress.
    // +required
    // +kubebuilder:validation:Enum=Install;Upgrade;Reconfigure
    Type OperationType `json:"type"`

    // bundle identifies the target bundle being rolled out.
    // +required
    Bundle BundleMetadata `json:"bundle"`
}
```

## Part 2: ClusterObjectSet Status Changes

### 2.1 Rename Available to Ready

The COS `Available` condition is renamed to `Ready` for consistency with the CE and to avoid implying service delivery semantics. "Ready" accurately conveys that all managed objects are on-cluster and passing their probes.

### 2.2 Convert Succeeded Condition to succeededAt Field

The `Succeeded` condition serves as a latch — set once when the rollout completes, never cleared. This is used by the CE controller to classify revisions as "Installed" vs "RollingOut." However, this latch behavior is unusual for a Kubernetes condition.

**Proposed**: Replace the `Succeeded` condition with a `succeededAt` timestamp field. The CE controller checks `cos.Status.SucceededAt != nil` instead of looking for a `Succeeded=True` condition.

```go
// succeededAt records when this revision first completed its rollout.
// Once set, it is never cleared — even if the revision later becomes unavailable.
// This field is used by the ClusterExtension controller to identify the "installed" revision.
// +optional
SucceededAt *metav1.Time `json:"succeededAt,omitempty"`
```

### 2.3 Add Per-Phase Status

A new `status.phases` array provides per-phase rollout progress. This is the primary mechanism for understanding where a rollout is stuck without inspecting individual managed objects.

```go
type PhaseStatus struct {
    // name is the phase name, matching an entry in spec.phases.
    // +required
    Name string `json:"name"`

    // status indicates the current state of this phase.
    // +required
    // +kubebuilder:validation:Enum=Pending;Active;Complete;Failed;Transitioning
    Status PhaseStatusState `json:"status"`

    // lastTransitionTime is the last time the phase status transitioned.
    // +required
    LastTransitionTime metav1.Time `json:"lastTransitionTime"`

    // message is a human-readable description of the phase's current state.
    // For failed phases, this includes details about probe failures.
    // +optional
    Message string `json:"message,omitempty"`
}

type PhaseStatusState string

const (
    // PhasePending indicates the phase has not been reconciled yet.
    // The revision engine has not reached this phase in the sequential rollout.
    PhasePending PhaseStatusState = "Pending"

    // PhaseActive indicates the phase is currently being reconciled.
    // Objects have been applied and the engine is waiting for probes to pass.
    PhaseActive PhaseStatusState = "Active"

    // PhaseComplete indicates all objects in this phase pass their probes.
    PhaseComplete PhaseStatusState = "Complete"

    // PhaseFailed indicates one or more objects in this phase failed their probes
    // or encountered an error. The message field contains details.
    PhaseFailed PhaseStatusState = "Failed"

    // PhaseTransitioning indicates some objects in this phase have been adopted
    // by a newer revision while others are still owned by this revision. This state
    // occurs on the OLD revision during a multi-revision upgrade.
    PhaseTransitioning PhaseStatusState = "Transitioning"
)
```

**Phase state transitions on a new revision (COS-2)**:
- **Pending → Active**: The revision engine begins reconciling this phase (adopting objects from the old revision or creating new ones).
- **Active → Complete**: All objects in the phase pass their progression probes.
- **Active → Failed**: One or more objects fail probes or encounter errors. The `Message` field contains details (e.g., "Deployment my-ns/my-deploy: updatedReplicas (1) != replicas (3)").
- **Failed → Active**: On retry, the phase re-enters Active state when the engine re-reconciles it.

**Phase state transitions on the old revision (COS-1) during upgrade**:
- **Complete → Transitioning**: A newer revision has adopted some (but not all) objects in this phase. The `Message` field shows transition progress (e.g., "2/5 objects transitioned to revision 2").
- **Transitioning → Complete**: All objects in this phase have been adopted by the newer revision (`HasProgressed()=true`). From COS-1's perspective, the phase is done — its objects are no longer its responsibility.

**How phases are populated**: The COS controller derives phase status from the boxcutter `RevisionResult` and the COS spec:

1. **All phase names** come from `cos.Spec.Phases[*].Name` — this is the complete list.
2. **Processed phases** come from `RevisionResult.GetPhases()` — this is a subset; the revision engine stops at the first incomplete phase on new revisions. For old revisions (with `succeededAt`), all phases are processed.
3. **Derivation rules for new revisions** (no `succeededAt`):
   - Phases in `spec.phases` but NOT in `RevisionResult.GetPhases()` → `Pending`
   - Phases in the result where `PhaseResult.IsComplete()` returns true → `Complete`
   - The last phase in the result where `!PhaseResult.IsComplete()`:
     - If any `ObjectResult.ProbeResults()` has a failing progress probe → `Failed`, with probe failure messages in the `Message` field
     - If `PhaseResult.InTransition()` returns true (objects are being applied/adopted) → `Active`
     - If `PhaseResult.GetValidationError()` is non-nil → `Failed`, with validation error in the `Message` field
   - Phases with `ObjectResult.Action() == ActionCollision` → `Failed`, with collision details in the `Message` field
4. **Derivation rules for old revisions** (has `succeededAt`):
   - `PhaseResult.HasProgressed()` returns true (all objects adopted by newer revision) → `Complete`
   - Phase has mix of `ActionProgressed` and `ActionIdle` objects → `Transitioning`, message shows progress count
   - All objects `ActionIdle` (no newer revision has adopted them) → `Complete`
5. **Pre-phase errors** (secret immutability, revision engine creation, reconcile error): all phases are `Pending` because the controller never reached phase processing.

**Phase names**: The COS uses human-readable phase names derived from the Group-Kind classification (e.g., `namespaces`, `crds`, `roles`, `deploy`, `publish`). These are stable and deterministic for a given bundle.

### 2.4 Updated COS Conditions

**COS Conditions (2 — down from 3)**:

| Condition | Status=True | Status=False | Status=Unknown |
|-----------|------------|-------------|----------------|
| **Ready** | `ProbesSucceeded` — all managed objects pass probes | `ProbeFailure` — one or more probe failures; `RollingOut` — rollout not yet complete | `Reconciling` — transient error; `Archived` — revision archived |
| **Progressing** | `RollingOut` — active rollout; `ObjectCollisionDetected` — object ownership conflict; `ValidationFailed` — preflight/dry-run failure; `Retrying` — other transient error | `Succeeded` — rollout complete; `Blocked` — terminal error; `Archived` — revision archived; `ProgressDeadlineExceeded` — deadline exceeded | — |

**Key changes**:
- `Progressing=True, Reason=Succeeded` (the same bug as CE) is fixed to `Progressing=False, Reason=Succeeded`.
- The `Ready` condition is always set on first reconcile, even if probes haven't been evaluated. Pre-phase errors set `Ready=Unknown/Reconciling` rather than leaving the condition absent. This follows the Kubernetes convention that controllers should signal awareness of a condition on first visit.

### 2.5 Updated COS Print Columns

**Current**: `Available`, `Progressing`, `Age`

**Proposed**: `Revision`, `Ready`, `Progressing`, `Reason`, `Age` (+ `Message` with `-o wide`)

| Column | JSONPath | Rationale |
|--------|---------|-----------|
| Revision | `.spec.revision` | Which revision number — essential for debugging multi-revision scenarios |
| Ready | `.status.conditions[?(@.type=='Ready')].status` | Health signal (renamed from Available) |
| Progressing | `.status.conditions[?(@.type=='Progressing')].status` | Is active work happening |
| Reason | `.status.conditions[?(@.type=='Progressing')].reason` | Why — `Archived` in the reason column replaces the need for a separate Lifecycle column. Matches the CE pattern for consistent triage |
| Message | `.status.conditions[?(@.type=='Progressing')].message` | **Wide only** (priority=1, shown with `-o wide`). Specific error detail for debugging |
| Age | `.metadata.creationTimestamp` | Standard — always last per kubectl convention |

The `Lifecycle` column is dropped because the `Reason` column already shows `Archived` for archived revisions — any other reason implies Active.

Example with multiple revisions including archived:

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   REASON      AGE
my-operator-1   1          Unknown   False         Archived    30d
my-operator-2   2          Unknown   False         Archived    5d
my-operator-3   3          True      False         Succeeded   1d
```

### 2.6 Complete COS Status Structure

```go
type ClusterObjectSetStatus struct {
    // conditions is a list of status conditions describing the state of the ClusterObjectSet.
    //
    // The Ready condition represents whether the revision's managed objects are healthy:
    //   - When status is True and reason is ProbesSucceeded, all managed objects pass their readiness probes.
    //   - When status is False and reason is ProbeFailure, one or more objects are failing their probes.
    //   - When status is False and reason is RollingOut, the rollout is in progress and readiness has not been established.
    //   - When status is Unknown and reason is Reconciling, a transient error prevented probe observation.
    //   - When status is Unknown and reason is Archived, the revision has been archived and its objects torn down.
    //
    // The Progressing condition represents whether the revision is actively rolling out:
    //   - When status is True and reason is RollingOut, the revision is actively making progress.
    //   - When status is True and reason is ObjectCollisionDetected, an object ownership conflict was detected.
    //   - When status is True and reason is ValidationFailed, a preflight or dry-run validation failed.
    //   - When status is True and reason is Retrying, the revision encountered a transient error and is retrying.
    //   - When status is False and reason is Succeeded, the revision has completed its rollout.
    //   - When status is False and reason is Blocked, the revision encountered a terminal error requiring manual intervention.
    //   - When status is False and reason is Archived, the revision has been archived.
    //   - When status is False and reason is ProgressDeadlineExceeded, the revision exceeded its progress deadline.
    //
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // succeededAt records when this revision first completed its rollout.
    // Once set, it is never cleared. Used by the ClusterExtension controller
    // to identify the "installed" revision.
    // +optional
    SucceededAt *metav1.Time `json:"succeededAt,omitempty"`

    // phases reports the rollout status of each phase in this revision.
    // Phases are reconciled sequentially; a phase must complete before the next begins.
    // +listType=map
    // +listMapKey=name
    // +optional
    Phases []PhaseStatus `json:"phases,omitempty"`

    // observedPhases records the content hashes of resolved phases
    // at first successful reconciliation. This is used to detect if
    // referenced object sources were deleted and recreated with
    // different content.
    // +kubebuilder:validation:XValidation:rule="self == oldSelf || oldSelf.size() == 0",message="observedPhases is immutable"
    // +kubebuilder:validation:MaxItems=20
    // +listType=map
    // +listMapKey=name
    // +optional
    ObservedPhases []ObservedPhase `json:"observedPhases,omitempty"`
}
```

## Part 3: Scenario Catalog and UX Examples

This section walks through every major scenario a user may encounter. For each scenario, we show:
- What `kubectl get clusterextensions` displays (print columns)
- What `kubectl describe clusterextension <name>` reveals (conditions + status fields)
- What the COS shows for deeper debugging (when relevant)
- What action the user should take

### 3.1 Happy Path: Steady State

The extension is installed and healthy. No work in progress.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON      VERSION   OPERATION   TARGET   AGE
my-operator   True    False         Succeeded   1.0.0                          5d
```

```yaml
# kubectl describe clusterextension my-operator (status excerpt)
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "False"
    reason: Succeeded
    message: "Desired state reached"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
```

**User action**: None. Everything is working.

---

### 3.2 Happy Path: First Install In Progress

A new ClusterExtension is being installed for the first time. The COS is rolling out phases sequentially.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON       VERSION   OPERATION   TARGET   AGE
my-operator   False   True          RollingOut   <none>    Install     1.0.0    30s
```

```yaml
status:
  conditions:
  - type: Installed
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "False"
    reason: Absent
    message: "No bundle installed yet — rollout in progress"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: RollingOut
    message: "Rolling out bundle my-operator v1.0.0"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation:
    type: Install
    bundle:
      name: my-operator
      version: 1.0.0
```

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   REASON       AGE
my-operator-1   1          False   True          RollingOut   30s
```

```yaml
# COS (for deeper debugging)
status:
  conditions:
  - type: Ready
    status: "False"
    reason: RollingOut
    message: "Managed resources are being updated"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: RollingOut
    message: "Revision 1.0.0 is rolling out."
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases:
  - name: namespaces
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - name: crds
    status: Active
    lastTransitionTime: "2026-07-07T10:00:05Z"
    message: "Waiting for CRD myresource.example.com: Established condition is not True"
  - name: roles
    status: Pending
  - name: deploy
    status: Pending
```

**User action**: Wait. The rollout is progressing normally.

---

### 3.3 Happy Path: Upgrade In Progress

The user changed the version constraint. A new COS revision is rolling out while the old version continues serving.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON       VERSION   OPERATION   TARGET   AGE
my-operator   False   True          RollingOut   1.0.0     Upgrade     2.0.0    5d
```

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "False"
    reason: RollingOut
    message: "Managed resources are being updated"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: RollingOut
    message: "Rolling out bundle my-operator v2.0.0"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   REASON       AGE
my-operator-1   1          True    False         Succeeded    5d
my-operator-2   2          False   True          RollingOut   30s
```

**Key UX point**: `Ready=False` — the new revision's objects are still rolling out, so the on-cluster state is in transition. `Installed=True` confirms the previous version was installed. `Version=1.0.0` shows what was installed, `Operation=Upgrade` and `Target=2.0.0` show where it's headed. The COS table shows two active revisions. Once COS-2 completes, Ready returns to True.

**User action**: Wait. Monitor Progressing condition for progress.

---

### 3.3a Happy Path: Upgrade — Phase-Level View of Both Revisions

This shows the detailed phase-level state during the same upgrade from 3.3, viewed from both COS revisions. COS-2 is in the middle of its phased rollout, adopting objects from COS-1 phase by phase.

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   REASON       AGE
my-operator-1   1          True    False         Succeeded    5d
my-operator-2   2          False   True          RollingOut   2m
```

```yaml
# COS my-operator-2 (new revision) — actively rolling out
status:
  conditions:
  - type: Ready
    status: "False"
    reason: RollingOut
    message: "Managed resources are being updated"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: RollingOut
    message: "Revision 2.0.0 is rolling out."
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases:
  - name: namespaces
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - name: crds
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:05Z"
  - name: roles
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:10Z"
  - name: deploy
    status: Active
    lastTransitionTime: "2026-07-07T10:00:15Z"
    message: "Waiting for Deployment my-ns/my-deploy: updatedReplicas (1) != replicas (3)"
  - name: publish
    status: Pending
```

```yaml
# COS my-operator-1 (old revision) — objects being handed off to COS-2
status:
  succeededAt: "2026-07-02T08:00:00Z"
  conditions:
  - type: Ready
    status: "True"
    reason: ProbesSucceeded
    message: "Objects are available and pass all probes."
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: Succeeded
    message: "Revision 1.0.0 has rolled out."
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases:
  - name: namespaces
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:01Z"
    message: "All objects transitioned to revision 2"
  - name: crds
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:06Z"
    message: "All objects transitioned to revision 2"
  - name: roles
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:11Z"
    message: "All objects transitioned to revision 2"
  - name: deploy
    status: Transitioning
    lastTransitionTime: "2026-07-07T10:00:15Z"
    message: "1/3 objects transitioned to revision 2"
  - name: publish
    status: Complete
    lastTransitionTime: "2026-07-02T08:00:00Z"
```

**Key UX point**: COS-2's phases show the new revision's rollout progress — `namespaces`, `crds`, and `roles` are Complete (objects adopted and probes pass), `deploy` is Active (waiting on probes), `publish` is Pending (not yet reached).

COS-1's phases show the handoff progress — `namespaces`, `crds`, and `roles` are Complete (all objects transitioned to revision 2), `deploy` is Transitioning (1 of 3 objects adopted by COS-2 so far), and `publish` is still Complete (COS-1 still owns these objects).

Once COS-2 completes all phases, it archives COS-1.

---

### 3.4 Happy Path: Reconfiguration In Progress

The user changed configuration (e.g., service account, inline config) without changing the version. A new COS revision is created with the same version.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON       VERSION   OPERATION     TARGET   AGE
my-operator   False   True          RollingOut   1.0.0     Reconfigure   1.0.0    5d
```

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "False"
    reason: RollingOut
    message: "Managed resources are being updated"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: RollingOut
    message: "Rolling out configuration change for bundle my-operator v1.0.0"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Reconfigure
    bundle:
      name: my-operator
      version: 1.0.0
```

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   REASON      AGE
my-operator-1   1          True    False         Succeeded   5d
my-operator-2   2          False   True          Retrying    10s
```

**User action**: Wait. The reconfiguration is in progress.

---

### 3.5 CE Error: Bundle Not Found (No Previous Install)

The user specifies a package name or version that doesn't exist in any catalog. Nothing was previously installed.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON             VERSION   OPERATION   TARGET   AGE
my-operator   False   True          ResolutionFailed   <none>                         2m
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON             VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   True          ResolutionFailed   <none>                         no bundles found for package \"my-operator\" matching version \">=9...   2m

```yaml
status:
  conditions:
  - type: Installed
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ResolutionFailed
    message: "no bundles found for package \"my-operator\" matching version \">=99.0.0\" in channels [stable]"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation: null
```

**Key UX point**: `Ready=False/Absent` (nothing deployed), `Progressing=True/Retrying` with the specific resolution error. The user checks Progressing to understand what went wrong. No COS exists to inspect. No `operation` is set because resolution hasn't succeeded yet (we don't know the target bundle).

**User action**: Fix the package name, version constraint, or channel in the CE spec. Or add a catalog containing the desired package.

---

### 3.6 CE Error: Bundle Not Found (During Upgrade)

The user requests an upgrade to a version that doesn't exist, but the old version is still running.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON             VERSION   OPERATION   TARGET   AGE
my-operator   True    True          ResolutionFailed   1.0.0                          5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON             VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    True          ResolutionFailed   1.0.0                          unable to upgrade to version >=99.0.0: no bundles found for package...   5d

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "True"
    reason: ResolutionFailed
    message: "unable to upgrade to version >=99.0.0: no bundles found for package \"my-operator\" matching version \">=99.0.0\" in channels [stable] (currently installed: v1.0.0)"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation: null
```

**Key UX point**: `Ready=True` — the old version is still healthy and unaffected. The upgrade error is entirely in `Progressing`. The message tells the user exactly what went wrong and what's currently installed.

**User action**: Fix the version constraint or wait for a catalog update that includes the desired version.

---

### 3.7 CE Error: Invalid Configuration (Terminal)

The user provides inline configuration that doesn't match the bundle's config schema.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON                 VERSION   OPERATION   TARGET   AGE
my-operator   True    False         InvalidConfiguration   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON                 VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    False         InvalidConfiguration   1.0.0     Upgrade     2.0.0    error for resolved bundle my-operator with version 2.0.0: invalid C...   5d

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "False"
    reason: InvalidConfiguration
    message: "error for resolved bundle my-operator with version 2.0.0: invalid ClusterExtension configuration: unknown field \"invalidKey\""
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

**Key UX point**: `Progressing=False` (not retrying — this is terminal). The `InvalidConfiguration` reason clearly tells the user this is a config problem, not a transient error. `Ready=True` — old version unaffected. `operation` persists so the user can see what they were trying to roll out to.

**User action**: Fix the inline configuration in the CE spec.

---

### 3.8 CE Error: Invalid Configuration (No Previous Install)

Same as above but nothing was previously installed.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON                 VERSION   OPERATION   TARGET   AGE
my-operator   False   False         InvalidConfiguration   <none>    Install     1.0.0    2m
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON                 VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   False         InvalidConfiguration   <none>    Install     1.0.0    error for resolved bundle my-operator with version 1.0.0: invalid C...   2m

```yaml
status:
  conditions:
  - type: Installed
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: InvalidConfiguration
    message: "error for resolved bundle my-operator with version 1.0.0: invalid ClusterExtension configuration: unknown field \"invalidKey\""
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation:
    type: Install
    bundle:
      name: my-operator
      version: 1.0.0
```

**Key UX point**: `Ready=False/Absent` (nothing deployed), `Progressing=False/InvalidConfiguration` — a concerning state. Both conditions are False, signaling a problem. The Progressing reason and message tell the user exactly what to fix. `operation` shows the target that failed.

**User action**: Fix the inline configuration.

---

### 3.9 CE Error: Image Pull Failure

The bundle image cannot be pulled (e.g., registry unreachable, auth failure, image doesn't exist).

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON            VERSION   OPERATION   TARGET   AGE
my-operator   False   True          ImagePullFailed   <none>    Install     1.0.0    5m
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON            VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   True          ImagePullFailed   <none>    Install     1.0.0    error for resolved bundle my-operator with version 1.0.0: error cop...   5m

```yaml
status:
  conditions:
  - type: Installed
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ImagePullFailed
    message: "error for resolved bundle my-operator with version 1.0.0: error copying image: authentication required"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation:
    type: Install
    bundle:
      name: my-operator
      version: 1.0.0
```

**User action**: Fix registry access, image pull secrets, or the image reference in the catalog.

---

### 3.10 CE Error: Image Pull Failure (Malformed Reference — Terminal)

The bundle's image reference string is malformed and cannot be parsed.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON    VERSION   OPERATION   TARGET   AGE
my-operator   False   False         Blocked   <none>    Install     1.0.0    2m
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON    VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   False         Blocked   <none>    Install     1.0.0    error for resolved bundle my-operator with version 1.0.0: error par...   2m

```yaml
status:
  conditions:
  - type: Installed
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: Blocked
    message: "error for resolved bundle my-operator with version 1.0.0: error parsing image reference \"!!!invalid\": invalid reference format"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation:
    type: Install
    bundle:
      name: my-operator
      version: 1.0.0
```

**Key UX point**: `Progressing=False/Blocked` — terminal. The image reference in the catalog is broken; no amount of retrying will fix it.

**User action**: Fix the bundle image reference in the catalog source.

---

### 3.11 CE Error: ServiceAccount Not Found

The ServiceAccount specified in `spec.serviceAccount.name` does not exist.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON             VERSION   OPERATION   TARGET   AGE
my-operator   False   True          ValidationFailed   <none>                         1m
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON             VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   True          ValidationFailed   <none>                         operation cannot proceed due to the following validation error(s): ...   1m

```yaml
status:
  conditions:
  - type: Installed
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ValidationFailed
    message: "operation cannot proceed due to the following validation error(s): service account \"my-sa\" not found in namespace \"my-ns\""
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation: null
```

**User action**: Create the ServiceAccount or fix the name/namespace in the CE spec.

---

### 3.12 CE Error: RBAC Insufficient (Pre-Authorization Failure)

The ServiceAccount exists but lacks RBAC permissions for the bundle's managed resources.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON                VERSION   OPERATION   TARGET   AGE
my-operator   True    True          AuthorizationFailed   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON                VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    True          AuthorizationFailed   1.0.0     Upgrade     2.0.0    error for resolved bundle my-operator with version 2.0.0: creating ...   5d

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "True"
    reason: AuthorizationFailed
    message: "error for resolved bundle my-operator with version 2.0.0: creating new Revision: pre-authorization failed: service account requires the following permissions: [create deployments.apps in namespace my-ns, create services in namespace my-ns]"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

**Key UX point**: `Ready=True` — old version unaffected. The Progressing message lists exactly which RBAC permissions are needed.

**User action**: Grant the listed permissions to the ServiceAccount.

---

### 3.13 CE Error: Unsupported Bundle Features

The bundle contains unsupported features like APIServiceDefinitions or unsupported install modes.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON               VERSION   OPERATION   TARGET   AGE
my-operator   True    True          UnsupportedContent   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON               VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    True          UnsupportedContent   1.0.0     Upgrade     2.0.0    error for resolved bundle my-operator with version 2.0.0: unsupport...   5d

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "True"
    reason: UnsupportedContent
    message: "error for resolved bundle my-operator with version 2.0.0: unsupported bundle: apiServiceDefinitions are not supported"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

**Key UX point**: `Ready=True` — old version unaffected. The error is only in `Progressing`. No COS is created for the new version because the error occurs before revision creation.

**User action**: Use a different bundle version that doesn't use unsupported features, or wait for feature support.

---

### 3.14 COS Error: Probe Failure (Deployment Not Ready)

The COS revision is stuck because a Deployment's pods are not ready (e.g., image pull backoff, crash loop).

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON         VERSION   OPERATION   TARGET   AGE
my-operator   False   True          ProbeFailure   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON         VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   True          ProbeFailure   1.0.0     Upgrade     2.0.0    Rolling out bundle my-operator v2.0.0: Object Deployment.apps/v1 my...   5d

```yaml
# CE status
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "False"
    reason: ProbeFailure
    message: "Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ProbeFailure
    message: "Rolling out bundle my-operator v2.0.0: Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   REASON       AGE
my-operator-1   1          True    False         Succeeded    5d
my-operator-2   2          False   True          RollingOut   5m
```

```yaml
# COS my-operator-2 status (deep debugging)
status:
  conditions:
  - type: Ready
    status: "False"
    reason: ProbeFailure
    message: "Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: RollingOut
    message: "Revision 2.0.0 is rolling out."
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases:
  - name: namespaces
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - name: crds
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:05Z"
  - name: roles
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:08Z"
  - name: deploy
    status: Failed
    lastTransitionTime: "2026-07-07T10:00:10Z"
    message: "Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
  - name: publish
    status: Pending
```

**Key UX point**: From the CE alone, the user can see an upgrade is happening and that a probe is failing — `Ready=False/ProbeFailure` shows the health issue on the latest COS, and the Progressing message includes the probe failure detail. The COS provides deeper phase-level debugging for users who need it.

**User action**: Investigate the Deployment (check pods, events, image availability).

---

### 3.15 COS Error: Object Collision

A managed object is already owned by another controller. The collision protection policy prevents adoption.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON     VERSION   OPERATION   TARGET   AGE
my-operator   True    True          Retrying   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON     VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    True          Retrying   1.0.0     Upgrade     2.0.0    Object collision in phase "roles": Deployment.apps/v1 my-ns/conflic...   5d

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   REASON                    AGE
my-operator-1   1          True      False         Succeeded                 5d
my-operator-2   2          Unknown   True          ObjectCollisionDetected   2m
```

```yaml
# CE status
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "True"
    reason: Retrying
    message: "Object collision in phase \"roles\": Deployment.apps/v1 my-ns/conflicting-deploy owned by ClusterObjectSet/other-ext-1"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

```yaml
# COS my-operator-2 status (deep debugging)
status:
  conditions:
  - type: Ready
    status: "Unknown"
    reason: Reconciling
    message: "Object collision in phase \"roles\": Deployment.apps/v1 my-ns/conflicting-deploy owned by ClusterObjectSet/other-ext-1"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ObjectCollisionDetected
    message: "Object collision in phase \"roles\": Deployment.apps/v1 my-ns/conflicting-deploy owned by ClusterObjectSet/other-ext-1"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases:
  - name: namespaces
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - name: crds
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:05Z"
  - name: roles
    status: Failed
    lastTransitionTime: "2026-07-07T10:00:08Z"
    message: "Object collision: Deployment.apps/v1 my-ns/conflicting-deploy owned by ClusterObjectSet/other-ext-1"
```

**Current vs proposed message format**: The current COS controller formats collision messages as `"revision object collisions in phase %d\n%s"` using the phase index and the raw `ObjectResult.String()` output from the boxcutter library. The boxcutter `ObjectResultCollision.String()` produces a verbose multi-line format:

```
Object Deployment.apps/v1 my-ns/conflicting-deploy
Action: "Collision"
Probes:
- Progress: Failed
  - "status.updatedReplicas" != "status.replicas"
Conflicting Owner: &OwnerReference{APIVersion:v1,Kind:ClusterObjectSet,Name:other-ext-1,...}
```

This RFC proposes the COS controller construct a cleaner, single-line message using the phase name (from `cos.Spec.Phases`) and the structured `ConflictingOwner()` accessor rather than the raw `String()` output:

```
Object collision in phase "roles": Deployment.apps/v1 my-ns/conflicting-deploy owned by ClusterObjectSet/other-ext-1
```

**Key UX point**: The CE surfaces the collision error — `Progressing=True/Retrying` with the collision details. The user can diagnose the conflict from the CE alone. `Ready=True` because the old version is still healthy.

**User action**: Resolve the resource ownership conflict (e.g., remove the conflicting resource from the other extension's bundle, or adjust collision protection).

---

### 3.16 COS Error: Secret Not Immutable (Blocked)

A Secret referenced by the COS is not marked as immutable. This is a terminal blocking error on the COS.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON    VERSION   OPERATION   TARGET   AGE
my-operator   False   False         Blocked   <none>    Install     1.0.0    5m
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON    VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   False         Blocked   <none>    Install     1.0.0    the following secrets are not immutable (referenced secrets must ha...   5m

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   REASON    AGE
my-operator-1   1          Unknown   False         Blocked   5m
```

```yaml
# CE status
status:
  conditions:
  - type: Installed
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: Blocked
    message: "the following secrets are not immutable (referenced secrets must have immutable set to true): my-ns/my-secret-1, my-ns/my-secret-2"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation:
    type: Install
    bundle:
      name: my-operator
      version: 1.0.0
```

```yaml
# COS my-operator-1 status
status:
  conditions:
  - type: Ready
    status: "Unknown"
    reason: Reconciling
    message: "Reconciliation blocked before probe evaluation"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: Blocked
    message: "the following secrets are not immutable (referenced secrets must have immutable set to true): my-ns/my-secret-1, my-ns/my-secret-2"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases: []   # no phases populated — reconciliation blocked before phase processing
```

**Key UX point**: The CE surfaces the COS blocking error directly — `Progressing=False/Blocked` with the specific error message. The user can diagnose the problem entirely from the CE without inspecting COS.

**User action**: Ensure all referenced Secrets have `immutable: true`.

---

### 3.17 COS Error: Phase Content Changed (Blocked)

A referenced Secret was deleted and recreated with different content after the COS's first successful resolution. The immutable digest check fails.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON    VERSION   OPERATION   TARGET   AGE
my-operator   True    False         Blocked   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON    VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    False         Blocked   1.0.0     Upgrade     2.0.0    resolved content of 1 phase(s) has changed: phase \"deploy\" (expec...   5d

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   REASON      AGE
my-operator-1   1          True      False         Succeeded   5d
my-operator-2   2          Unknown   False         Blocked     1h
```

```yaml
# CE status
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "False"
    reason: Blocked
    message: "resolved content of 1 phase(s) has changed: phase \"deploy\" (expected digest sha256:abc123, got sha256:def456); a referenced object source may have been deleted and recreated with different content"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

```yaml
# COS my-operator-2 status
status:
  conditions:
  - type: Ready
    status: "Unknown"
    reason: Reconciling
    message: "Reconciliation blocked before probe evaluation"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: Blocked
    message: "resolved content of 1 phase(s) has changed: phase \"deploy\" (expected digest sha256:abc123, got sha256:def456); a referenced object source may have been deleted and recreated with different content"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases: []   # no phases populated — blocked before processing
```

**Key UX point**: The CE surfaces the COS blocking error directly — `Progressing=False/Blocked`. The user can diagnose the problem from the CE alone. `Ready=True` because the old version is still healthy.

**User action**: A new COS revision must be created (typically by triggering a reconciliation of the CE).

---

### 3.18 COS Error: Validation Error (Preflight)

Boxcutter preflight validation fails (e.g., dry-run apply rejected by admission controller).

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON     VERSION   OPERATION   TARGET   AGE
my-operator   True    True          Retrying   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON     VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    True          Retrying   1.0.0     Upgrade     2.0.0    revision validation error: dry-run apply rejected by webhook: admis...   5d

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   REASON             AGE
my-operator-1   1          True      False         Succeeded          5d
my-operator-2   2          Unknown   True          ValidationFailed   3m
```

```yaml
# CE status
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "True"
    reason: Retrying
    message: "revision validation error: dry-run apply rejected by webhook: admission controller denied the request"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

```yaml
# COS my-operator-2 status (deep debugging)
status:
  conditions:
  - type: Ready
    status: "Unknown"
    reason: Reconciling
    message: "revision validation error: dry-run apply rejected by webhook: admission controller denied the request"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ValidationFailed
    message: "revision validation error: dry-run apply rejected by webhook: admission controller denied the request"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases:
  - name: namespaces
    status: Pending
  - name: crds
    status: Pending
  - name: deploy
    status: Pending
```

**Key UX point**: The CE surfaces the COS validation error directly — `Progressing=True/Retrying` with the specific error. The user can diagnose the problem from the CE alone. All COS phases are `Pending` because the revision-level preflight failed before any phase was processed.

**User action**: Investigate the validation error (check admission webhooks, resource schemas).

---

### 3.19 Progress Deadline Exceeded (First Install)

The first install has been stuck for longer than the configured progress deadline.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON                     VERSION   OPERATION   TARGET   AGE
my-operator   False   False         ProgressDeadlineExceeded   <none>    Install     1.0.0    35m
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON                     VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   False         ProgressDeadlineExceeded   <none>    Install     1.0.0    Revision has not rolled out for 30 minute(s). Last status: Revision...   35m

```yaml
# CE status
status:
  conditions:
  - type: Installed
    status: "False"
    reason: Absent
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "False"
    reason: ProbeFailure
    message: "Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: ProgressDeadlineExceeded
    message: "Revision has not rolled out for 30 minute(s). Last status: Revision 1.0.0 is rolling out."
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation:
    type: Install
    bundle:
      name: my-operator
      version: 1.0.0
```

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   REASON                     AGE
my-operator-1   1          False   False         ProgressDeadlineExceeded   35m
```

```yaml
# COS my-operator-1 status
status:
  conditions:
  - type: Ready
    status: "False"
    reason: ProbeFailure
    message: "Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: ProgressDeadlineExceeded
    message: "Revision has not rolled out for 30 minute(s). Last status: Revision 1.0.0 is rolling out."
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  phases:
  - name: namespaces
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - name: crds
    status: Complete
    lastTransitionTime: "2026-07-07T10:00:05Z"
  - name: deploy
    status: Failed
    lastTransitionTime: "2026-07-07T10:05:00Z"
    message: "Deployment.apps/v1 my-ns/my-deploy: image pull backoff for image quay.io/example/my-operator:v1.0.0"
  - name: publish
    status: Pending
```

**Key UX point**: `Progressing=False` with `ProgressDeadlineExceeded` — the controller has given up retrying. `Ready=False/ProbeFailure` shows exactly what's wrong. The COS table shows `Ready=False, Progressing=False` — a red flag. The COS phase status shows the `deploy` phase is stuck.

**User action**: Investigate the probe failure (check Deployment, pods, images, resource limits).

---

### 3.20 Progress Deadline Exceeded (During Upgrade)

An upgrade has been stuck too long. The old version is still installed and healthy.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON                     VERSION   OPERATION   TARGET   AGE
my-operator   False   False         ProgressDeadlineExceeded   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON                     VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   False   False         ProgressDeadlineExceeded   1.0.0     Upgrade     2.0.0    Revision has not rolled out for 30 minute(s). Last status: Revision...   5d

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "False"
    reason: ProbeFailure
    message: "Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: ProgressDeadlineExceeded
    message: "Revision has not rolled out for 30 minute(s). Last status: Revision 2.0.0 is rolling out."
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   REASON                     AGE
my-operator-1   1          True    False         Succeeded                  5d
my-operator-2   2          False   False         ProgressDeadlineExceeded   35m
```

**Key UX point**: `Ready=False/ProbeFailure` — the CE surfaces the actual probe failure from the latest COS. `Progressing=False/ProgressDeadlineExceeded` — the upgrade timed out. `operation` shows the failed target. `Installed=True` confirms the previous version was installed. The COS table shows the new revision (`my-operator-2`) is `Ready=False, Progressing=False` — a clear indicator of the stuck revision.

**User action**: Investigate the probe failure on the CE, or inspect the COS `my-operator-2` for phase-level details.

---

### 3.21 Catalog Unavailable (Graceful Degradation)

All catalogs have been deleted but the extension has a previously installed version. The controller falls back to maintaining the existing workload.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON      VERSION   OPERATION   TARGET   AGE
my-operator   True    False         Succeeded   1.0.0                          5d
```

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: Succeeded
    message: "Desired state reached"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Deprecated
    status: "Unknown"
    reason: DeprecationStatusUnknown
    message: "Catalog data unavailable"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation: null
```

**Key UX point**: Everything looks healthy. The `Deprecated=Unknown` condition is the only hint that catalog data is unavailable. The workload continues running undisturbed.

**User action**: Restore catalogs if upgrades are needed. No immediate action required.

---

### 3.22 CRD Upgrade Safety Check Failure

The upgrade includes CRD changes that fail the safety check (e.g., removing a stored version, removing a field).

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON            VERSION   OPERATION   TARGET   AGE
my-operator   True    True          PreflightFailed   1.0.0     Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON            VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    True          PreflightFailed   1.0.0     Upgrade     2.0.0    error for resolved bundle my-operator with version 2.0.0: CRD upgra...   5d

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "True"
    reason: PreflightFailed
    message: "error for resolved bundle my-operator with version 2.0.0: CRD upgrade safety check failed: stored version v1alpha1 removed in upgrade"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation:
    type: Upgrade
    bundle:
      name: my-operator
      version: 2.0.0
```

**Key UX point**: `Ready=True` — old version unaffected. The CRD safety check happens before a COS revision is created, so no COS exists for the new version.

**User action**: Set `spec.install.preflight.crdUpgradeSafety.enforcement: None` if the CRD change is intentional, or use a different bundle version.

---

### 3.23 Helm Storage Migration Error

During migration from Helm to boxcutter storage, the migration step fails.

```
$ kubectl get clusterextensions
NAME          READY   PROGRESSING   REASON     VERSION   OPERATION   TARGET   AGE
my-operator   True    True          Retrying   1.0.0                          30d
```

```
$ kubectl get clusterextensions -o wide
NAME          READY   PROGRESSING   REASON     VERSION   OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   True    True          Retrying   1.0.0                          migrating storage: listing ClusterObjectSets before attempting migr...   30d

```yaml
status:
  conditions:
  - type: Installed
    status: "True"
    reason: Succeeded
    message: "Installed bundle quay.io/example/my-operator:v1.0.0 successfully"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Ready
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "True"
    reason: Retrying
    message: "migrating storage: listing ClusterObjectSets before attempting migration: context deadline exceeded"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation: null
```

**Key UX point**: `Ready=True` — the existing Helm-managed workload is unaffected. The migration is retrying. No COS exists yet (migration creates the first one).

**User action**: Check the controller logs for migration details. This is typically a transient error that resolves on retry.

---

### 3.24 Multiple Extensions — Table View

The print columns work well for managing multiple extensions at scale:

```
$ kubectl get clusterextensions
NAME              READY   PROGRESSING   REASON       VERSION   OPERATION   TARGET   AGE
cert-manager      True    False         Succeeded    1.14.0                         30d
my-operator       False   True          RollingOut   1.0.0     Upgrade     2.0.0    5d
broken-operator   False   False         Blocked      <none>    Install     1.0.0    2h
deprecated-op     True    False         Succeeded    3.2.1                          90d
```

At a glance:
- `cert-manager`: Healthy, nothing happening — clean columns
- `my-operator`: NOT ready (upgrade in progress), upgrading to 2.0.0 — objects in transition
- `broken-operator`: NOT ready, NOT progressing, stuck on Install → 1.0.0 — needs attention
- `deprecated-op`: Healthy, nothing happening (check deprecation conditions for details)

**Distinguishing upgrade-in-progress from stuck**: Both `my-operator` and `broken-operator` show `Ready=False`. The difference is `Progressing`: True means active work (normal), False means stuck (needs attention). The pattern to watch for is `Ready=False, Progressing=False` — that's the red flag.

## Part 4: Condition Type and Reason Reference

This section provides a complete inventory of all condition types and reasons after this RFC is implemented, including where each constant is defined.

### 4.1 Shared Constants (`common_types.go`)

These constants are used by both ClusterExtension and ClusterObjectSet.

**Condition Types:**

| Constant | Value | Used by |
|----------|-------|---------|
| `TypeInstalled` | `"Installed"` | CE |
| `TypeReady` | `"Ready"` | CE, COS |
| `TypeProgressing` | `"Progressing"` | CE, COS |

**Reasons:**

| Constant | Value | Used on | CE | COS |
|----------|-------|---------|-----|-----|
| `ReasonSucceeded` | `"Succeeded"` | Installed=True, Ready=True, Progressing=False | ✓ | ✓ |
| `ReasonAbsent` | `"Absent"` | Installed=False, Ready=False | ✓ | — |
| `ReasonPending` | `"Pending"` | Ready=Unknown | ✓ | — |
| `ReasonProbeFailure` | `"ProbeFailure"` | Ready=False, Progressing=True (COS probes failing during rollout) | ✓ | ✓ |
| `ReasonRollingOut` | `"RollingOut"` | Progressing=True, Ready=False | ✓ | ✓ |
| `ReasonResolutionFailed` | `"ResolutionFailed"` | Progressing=True | ✓ | — |
| `ReasonImagePullFailed` | `"ImagePullFailed"` | Progressing=True | ✓ | — |
| `ReasonValidationFailed` | `"ValidationFailed"` | Progressing=True | ✓ | ✓ |
| `ReasonAuthorizationFailed` | `"AuthorizationFailed"` | Progressing=True | ✓ | — |
| `ReasonUnsupportedContent` | `"UnsupportedContent"` | Progressing=True | ✓ | — |
| `ReasonPreflightFailed` | `"PreflightFailed"` | Progressing=True | ✓ | — |
| `ReasonRetrying` | `"Retrying"` | Progressing=True (COS-level transient) | ✓ | ✓ |
| `ReasonBlocked` | `"Blocked"` | Progressing=False | ✓ | ✓ |
| `ReasonInvalidConfiguration` | `"InvalidConfiguration"` | Progressing=False | ✓ | — |
| `ReasonProgressDeadlineExceeded` | `"ProgressDeadlineExceeded"` | Progressing=False | ✓ | ✓ |

**New:** `TypeReady`, `ReasonPending`, `ReasonProbeFailure` (moved from COS-only to shared).

**Removed from COS-specific:** `ClusterObjectSetReasonProbeFailure` — replaced by shared `ReasonProbeFailure`.

### 4.2 CE-Specific Constants (`clusterextension_types.go`)

**Condition Types:**

| Constant | Value |
|----------|-------|
| `TypeDeprecated` | `"Deprecated"` |
| `TypePackageDeprecated` | `"PackageDeprecated"` |
| `TypeChannelDeprecated` | `"ChannelDeprecated"` |
| `TypeBundleDeprecated` | `"BundleDeprecated"` |

**Reasons:**

| Constant | Value | Used on |
|----------|-------|---------|
| `ReasonDeprecated` | `"Deprecated"` | Deprecated conditions=True |
| `ReasonNotDeprecated` | `"NotDeprecated"` | Deprecated conditions=False |
| `ReasonDeprecationStatusUnknown` | `"DeprecationStatusUnknown"` | Deprecated conditions=Unknown |

### 4.3 COS-Specific Constants (`clusterobjectset_types.go`)

**Condition Types:** None — all COS condition types are now shared (`TypeReady`, `TypeProgressing`).

**Removed:** `ClusterObjectSetTypeAvailable` (renamed to shared `TypeReady`), `ClusterObjectSetTypeSucceeded` (replaced by `succeededAt` field).

**Reasons:**

| Constant | Value | Used on |
|----------|-------|---------|
| `ClusterObjectSetReasonArchived` | `"Archived"` | Progressing=False, Ready=Unknown |
| `ClusterObjectSetReasonProbesSucceeded` | `"ProbesSucceeded"` | Ready=True |
| `ClusterObjectSetReasonReconciling` | `"Reconciling"` | Ready=Unknown |
| `ClusterObjectSetReasonObjectCollisionDetected` | `"ObjectCollisionDetected"` | Progressing=True |

**Removed:** `ClusterObjectSetReasonBlocked` (use shared `ReasonBlocked`), `ClusterObjectSetReasonProbeFailure` (use shared `ReasonProbeFailure`), `ClusterObjectSetReasonRetrying` (use shared `ReasonRetrying`).

**Dropped:** `"Migrated"` — documented in previous API comments but never implemented in code. With `succeededAt` replacing the Succeeded condition, migrated revisions simply get `succeededAt` set.

### 4.4 Complete Condition × Reason Matrix

**ClusterExtension:**

| Condition | True | False | Unknown |
|-----------|------|-------|---------|
| **Installed** | Succeeded | Absent | — |
| **Ready** | Succeeded | Absent, ProbeFailure, RollingOut | Pending |
| **Progressing** | RollingOut, ProbeFailure, ResolutionFailed, ImagePullFailed, ValidationFailed, AuthorizationFailed, UnsupportedContent, PreflightFailed, Retrying | Succeeded, Blocked, InvalidConfiguration, ProgressDeadlineExceeded | — |
| **Deprecated** | Deprecated | NotDeprecated | DeprecationStatusUnknown |
| **PackageDeprecated** | Deprecated | NotDeprecated | DeprecationStatusUnknown |
| **ChannelDeprecated** | Deprecated | NotDeprecated | DeprecationStatusUnknown |
| **BundleDeprecated** | Deprecated | NotDeprecated | DeprecationStatusUnknown, Absent |

**ClusterObjectSet:**

| Condition | True | False | Unknown |
|-----------|------|-------|---------|
| **Ready** | ProbesSucceeded | ProbeFailure, RollingOut | Reconciling, Archived |
| **Progressing** | RollingOut, ObjectCollisionDetected, ValidationFailed, Retrying | Succeeded, Blocked, Archived, ProgressDeadlineExceeded | — |

### 4.5 Reason Semantics

| Reason | Meaning | Retryable? |
|--------|---------|-----------|
| `Succeeded` | Operation completed successfully | N/A (terminal success) |
| `Absent` | Resource does not exist yet (neutral, not an error) | N/A |
| `Pending` | Waiting for initial state to be established | N/A |
| `RollingOut` | Active phased rollout in progress, no issues | N/A (progressing) |
| `ProbeFailure` | One or more readiness probes failing (on Ready: health; on Progressing: rollout stuck on probes) | Context-dependent |
| `ResolutionFailed` | Bundle resolution failed (package/version not found, ambiguous) | Yes |
| `ImagePullFailed` | Bundle image pull failed (auth, network, missing image) | Yes |
| `ValidationFailed` | CE validation failed (ServiceAccount not found, etc.) | Yes |
| `AuthorizationFailed` | RBAC pre-authorization failed (ServiceAccount lacks permissions) | Yes |
| `UnsupportedContent` | Bundle content unsupported (apiServiceDefinitions, install modes) | Yes (but may persist until bundle changes) |
| `PreflightFailed` | Preflight check failed (CRD upgrade safety, etc.) | Yes (but may persist until bundle or config changes) |
| `ObjectCollisionDetected` | Object ownership conflict — another controller owns the resource | Yes |
| `ValidationFailed` | Preflight or dry-run validation failed (on CE: SA not found, etc.; on COS: admission webhook, etc.) | Yes |
| `Retrying` | COS-level transient error (secret resolution, watch setup, engine error) | Yes |
| `Blocked` | Terminal error requiring manual intervention | No |
| `InvalidConfiguration` | User configuration error requiring spec change | No |
| `ProgressDeadlineExceeded` | Rollout exceeded configured time limit | No |
| `ProbesSucceeded` | All readiness probes passing | N/A (healthy) |
| `Reconciling` | Transient error during reconciliation | Yes |
| `Archived` | Revision has been archived (inactive) | N/A |
| `Deprecated` | Package/channel/bundle is deprecated | N/A |
| `NotDeprecated` | Package/channel/bundle is not deprecated | N/A |
| `DeprecationStatusUnknown` | Cannot determine deprecation status | N/A |

## Part 5: Implementation Plan

### 5.1 CE Progressing Semantics Fix (Bug Fix)

This can be shipped independently as a bug fix since the current `Progressing=True/Succeeded` behavior contradicts Kubernetes conventions.

**Changes**:
- `common_controller.go`: Change `setStatusProgressing()` to set `Progressing=False` when `err == nil`
- `clusterextension_reconcile_steps.go`: Update the Helm `ApplyBundle` step accordingly
- `boxcutter_reconcile_steps.go`: Update mirrored Progressing condition handling
- Update condition documentation in type comments
- Update all tests expecting `Progressing=True/Succeeded`

### 5.2 COS Status Enhancements (Experimental)

**Changes**:
- `common_types.go`:
  - Add `TypeReady = "Ready"` shared condition type
  - Move `ReasonProbeFailure = "ProbeFailure"` from COS-specific to shared
  - Add `ReasonPending = "Pending"` reason
- `clusterobjectset_types.go`:
  - Remove `ClusterObjectSetTypeAvailable` (replaced by shared `TypeReady`)
  - Remove `ClusterObjectSetTypeSucceeded` condition constant
  - Remove `ClusterObjectSetReasonProbeFailure` (replaced by shared `ReasonProbeFailure`)
  - Remove `ClusterObjectSetReasonBlocked` (replaced by shared `ReasonBlocked`)
  - Add `ClusterObjectSetReasonObjectCollisionDetected` and `ClusterObjectSetReasonValidationFailed` reasons
  - Add `SucceededAt *metav1.Time` field to `ClusterObjectSetStatus`
  - Add `Phases []PhaseStatus` field to `ClusterObjectSetStatus`
  - Add `PhaseStatus` and `PhaseStatusState` types
  - Update print columns to add Revision, Reason, and Message[wide]; rename Available→Ready; drop Lifecycle (redundant with Reason=Archived)
  - Remove `"Migrated"` from API doc comments (never implemented)
- `clusterobjectset_controller.go`:
  - Set `SucceededAt` timestamp instead of `Succeeded` condition
  - Populate `status.phases` from `RevisionResult.GetPhases()`, `PhaseResult`, and `cos.Spec.Phases` data
  - Fix `Progressing=True/Succeeded` → `Progressing=False/Succeeded`
  - Rename all Available-related helper functions to Ready
  - Update all reason constant references to use shared constants where applicable
- Update all tests

### 5.3 CE Status Enhancements

**Changes**:
- `clusterextension_types.go`:
  - Add `ClusterExtensionOperationStatus` type and `OperationType` enum
  - Add `Operation` field to `ClusterExtensionStatus`
  - Remove `ActiveRevisions` field and `RevisionStatus` type
  - Update print columns (Ready, Progressing, Reason, Version, Rollout, Target, Age, Message[wide])
- `common_controller.go`:
  - Add `setReadyCondition()` helper functions
  - Update `setInstalledStatusFromRevisionStates()` to also set Ready
  - Ready derivation: reflect installed COS probe state when installed; reflect pipeline error state when not installed
- `boxcutter_reconcile_steps.go`:
  - Stop mirroring COS conditions; translate COS state into CE-native Ready/Progressing
  - Populate `status.operation` with type determination logic (Install/Upgrade/Reconfigure)
  - Remove `activeRevisions` population
  - Use `cos.Status.SucceededAt != nil` instead of `Succeeded=True` condition for installed classification
- `clusterextension_controller.go`:
  - Update `SetDeprecationStatus` (unchanged but verify no interaction)
  - Update `ensureFailureConditionsWithReason` for new condition set (add Ready)
- `conditionsets/conditionsets.go`:
  - Add `TypeReady` to `ConditionTypes`
  - Add `ReasonProbeFailure`, `ReasonPending`, `ReasonResolutionFailed`, `ReasonImagePullFailed`, `ReasonValidationFailed`, `ReasonAuthorizationFailed`, `ReasonUnsupportedContent`, `ReasonPreflightFailed` to `ConditionReasons`
- Update all tests

### 5.4 Documentation and Migration

- Update API reference documentation
- Add upgrade/migration notes for consumers of the old condition set
- Update any tooling or scripts that depend on the old condition types

## Part 6: Recommended Events

Conditions capture the current state; events capture the history of what happened and when. Together they give operators a complete picture. This section recommends which state transitions should emit Kubernetes Events.

### 6.1 CE Events

Events on the ClusterExtension provide a time-series trail visible via `kubectl describe clusterextension <name>` or `kubectl get events`.

**Recommended event triggers:**

| Transition | Type | Reason | Example Message |
|------------|------|--------|-----------------|
| Rollout started | Normal | RolloutStarted | `"Starting upgrade to bundle my-operator v2.0.0"` |
| Rollout completed | Normal | RolloutCompleted | `"Successfully rolled out bundle my-operator v2.0.0"` |
| Rollout failed (terminal) | Warning | RolloutFailed | `"Rollout blocked: invalid ClusterExtension configuration: unknown field \"invalidKey\""` |
| Progressing reason changed | Warning | ProgressingReasonChanged | `"Progressing reason changed from RollingOut to ProbeFailure: Deployment my-ns/my-deploy not ready"` |
| Resolution failed | Warning | ResolutionFailed | `"No bundles found for package \"my-operator\" matching version \">=99.0.0\" in channels [stable]"` |
| Image pull failed | Warning | ImagePullFailed | `"Error copying image: authentication required"` |
| Authorization failed | Warning | AuthorizationFailed | `"Pre-authorization failed: service account requires permissions: [create deployments.apps]"` |
| Progress deadline exceeded | Warning | ProgressDeadlineExceeded | `"Revision has not rolled out for 30 minute(s)"` |

**Guidance:**
- Use `Normal` type for expected lifecycle transitions (start, complete)
- Use `Warning` type for errors and unexpected state changes
- Include the specific error in the message — events are often the first thing SREs check after an alert
- Event reasons should match the Progressing condition reasons for consistency

### 6.2 COS Events

Events on the ClusterObjectSet provide phase-level debugging context.

**Recommended event triggers:**

| Transition | Type | Reason | Example Message |
|------------|------|--------|-----------------|
| Phase completed | Normal | PhaseComplete | `"Phase \"crds\" complete (2/5 phases done)"` |
| Phase failed (probe) | Warning | ProbeFailure | `"Phase \"deploy\" probe failure: Deployment my-ns/my-deploy: updatedReplicas (0) != replicas (3)"` |
| Object collision | Warning | ObjectCollisionDetected | `"Object collision in phase \"roles\": Deployment my-ns/deploy owned by ClusterObjectSet/other-ext-1"` |
| Revision blocked | Warning | Blocked | `"Revision blocked: referenced secrets are not immutable"` |
| Revision archived | Normal | Archived | `"Revision archived — superseded by revision 3"` |
| Revision succeeded | Normal | Succeeded | `"Revision 2 rolled out successfully"` |
| Progress deadline exceeded | Warning | ProgressDeadlineExceeded | `"Revision has not rolled out for 30 minute(s)"` |

**Guidance:**
- Phase completion events give SREs a timeline of rollout progress without having to watch the resource
- Error events on the COS should include enough context for diagnosis — the SRE may be looking at events across many resources via `kubectl get events --field-selector involvedObject.kind=ClusterObjectSet`
- Keep events concise — the condition message carries the full detail

### 6.3 Implementation Notes

- Event emission is an implementation concern — the exact event types and message formats are not part of the API contract
- Events should be emitted on **transitions**, not on every reconcile (avoid flooding the event stream)
- Consider deduplication — repeated probe failures should not emit a new event every 10 seconds; one event with an incrementing count is sufficient
- Events are retained by the Kubernetes event TTL (default 1 hour) — they complement but do not replace conditions for persistent state

# **Benefit**

1. **Self-sufficient CE**: Users can understand extension health, installation status, and rollout progress entirely from the CE, without inspecting COS resources. The `Ready` condition provides a clear health signal, and `status.operation` shows upgrade context.

2. **Correct semantics**: `Progressing=False/Succeeded` follows Kubernetes conventions. Users and tooling (like `kubectl wait --for=condition=Progressing=False`) work as expected.

3. **Phase-level debugging on COS**: When users do need to investigate a stuck rollout, COS provides per-phase status with probe failure details, eliminating the need to inspect individual managed objects.

4. **Better kubectl experience**: Print columns show what matters — `Ready`, `Progressing`, and `Reason` give an at-a-glance health summary and triage signal, `Version` shows what's installed, and `Operation`/`Target` show what's happening.

5. **Cleaner API boundary**: CE no longer leaks COS implementation details (no mirrored conditions, no `activeRevisions`). The API contract is between the user and the CE; COS is purely internal.

# **Competition** (alternatives)

### Alternative 1: Do Nothing

Keep the current status model. Users continue inspecting COS for deployment details and working around the `Progressing=True/Succeeded` semantics.

**Why not**: The Progressing semantics bug will confuse every new user and every automation tool. The lack of a health signal on CE means users always need two `kubectl` commands to understand their extension state. The cost of maintaining the status quo increases as adoption grows.

### Alternative 2: Message Enrichment Only

Fix the Progressing semantics and add phase info to condition messages, but add no new API fields.

**Why not**: Phase information and rollout type are not programmatically accessible. Tooling cannot parse free-text condition messages reliably. This approach helps human readers but not automation.

### Alternative 3: Full CE Self-Sufficiency (Phase Detail on CE)

Add per-phase status fields on CE as well as COS, making CE completely self-sufficient even for deep debugging.

**Why not**: This duplicates COS data on the CE, couples CE's API surface to COS internals (phase names, phase semantics), and increases maintenance burden. The 90/10 rule applies — CE's `Ready` + `Progressing` + `status.operation` handle 90% of cases; the remaining 10% (deep phase debugging) is appropriately served by COS.

# **Non-goals**

- **Service-level health checks**: OLM manages Kubernetes resources, not application health. The `Ready` condition reflects resource-level probe status, not application-level availability. Application-level health monitoring is out of scope.

- **Historical rollout tracking**: This RFC does not propose rollout history on the CE. Past rollouts can be observed through COS resources (retained up to the retention limit of 5) and Kubernetes events.

- **COS spec changes**: This RFC only modifies COS status fields. COS spec (phases, probes, collision protection) is unchanged.

- **Helm applier changes**: The status improvements focus on the boxcutter applier path. The Helm applier is being deprecated and is not covered by the COS phase status changes.

# **Key Dependencies and Open Questions**

### Resolved Questions

1. **Boxcutter library phase data**: ✅ Resolved. The boxcutter `RevisionResult` interface provides `GetPhases() []PhaseResult`, where each `PhaseResult` has `GetName()`, `IsComplete()`, `InTransition()`, `GetValidationError()`, and `GetObjects() []ObjectResult`. Each `ObjectResult` has `ProbeResults() ProbeResultContainer` with per-probe status and messages. The implementation maps these to `PhaseStatus` as described in §2.3. Phase names for "Pending" phases come from `cos.Spec.Phases[*].Name` (the full list) minus the phases in `RevisionResult.GetPhases()` (only processed phases).

2. **Rollout field lifecycle**: ✅ Resolved. `status.operation` persists whenever a rollout has been attempted, including failed/blocked rollouts. It is cleared only on `Progressing=False/Succeeded`. This ensures the user can always see what they were trying to roll out to, even if it failed.

3. **Ready condition during upgrades**: ✅ Resolved. Ready tracks the latest active COS's probe state to reflect the actual on-cluster state. During an upgrade, Ready drops to `False/RollingOut` because objects are in transition between revisions. When a pre-COS error occurs and no new COS is created, Ready stays based on the installed revision (the on-cluster state hasn't changed). Ready does not carry pipeline error detail — that is Progressing's job. This follows the Kubernetes Deployment pattern where Available and Progressing are orthogonal. The pattern `Ready=False, Progressing=True` is normal during upgrades; the red flag is `Ready=False, Progressing=False` (stuck).

4. **Phase status for migrated revisions**: ✅ Resolved. Migrated COS revisions (from `BoxcutterStorageMigrator`) did not go through the phased rollout process. Their `status.phases` will be empty, and they will have `succeededAt` set. This is acceptable because migrated revisions represent pre-existing workloads that were already running.

### Open Questions

1. **Backward compatibility for condition consumers**: Tooling that watches for `Progressing=True/Succeeded` will break when the semantics change to `Progressing=False/Succeeded`. This needs to be communicated clearly in release notes. Since the COS API and the experimental CE fields are not yet GA, this is less of a concern for those changes. The CE `Progressing` fix is a bug fix and should be documented as such.

2. **Phase status message size**: Probe failure messages can be verbose (they include full GVK, namespace/name, and probe details). For phases with many objects, the concatenated message could be large. The current COS controller breaks after the first failing object per phase. We should adopt a similar strategy and potentially limit message size.

3. **Ready condition and resource drift**: Since Ready now tracks the latest active COS's probe state, drift on the installed revision would be reflected if the COS re-reconciles and temporarily reports probes failing. This is consistent with the "track actual on-cluster state" principle — if probes are temporarily failing, Ready should reflect that. The condition will recover once the COS re-reconciles and probes pass again.

4. **Rollout type for downgrades**: The RFC defines `Install`, `Upgrade`, and `Reconfigure` rollout types. Should we add a `Downgrade` type for cases where the target version is lower than the installed version? Or is `Upgrade` sufficient (it's still a version change, just in the other direction)?

# **RACI**

| R​esponsible | TBD |
| :---- | :---- |
| **A**​ccountable | TBD |
| **C**​onsulted | OLM v1 team, UX team |
| **I**​nformed | OLM community |
