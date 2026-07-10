# **RFC**: ClusterExtension and ClusterObjectSet Status Redesign

## Improving Status Observability: Making CE Self-Sufficient and COS Phase-Aware

**Author Name(s)**: Per G. da Silva
**Author Date**: July 7, 2026
**Feedback Due Date**: **July 21, 2026**
**Status:** Draft
**Approval:** TBD
**Parent Brief:** \<none\>

# **Need**

The ClusterExtension (CE) is the user-facing API for managing operator lifecycle in OLM v1. From a product perspective, the ClusterObjectSet (COS) is an implementation detail — it is the mechanism the CE controller uses to apply and track managed resources, but users should never need to know it exists. The CE's status surface should be **self-sufficient**: a user should be able to understand, diagnose, and act on their extension's state entirely from the CE, without inspecting COS resources or any other internal objects.

Today, this abstraction boundary is broken. The CE status surface has several issues that force users behind the curtain and undermine integration with standard Kubernetes workflows.

### The CE is a leaky abstraction for the COS

The CE controller mirrors COS `Available` and `Progressing` conditions directly onto the CE, including COS-specific reasons like `ProbesSucceeded` and `Deploying`. This exposes COS internals through the CE's API surface — if the COS condition vocabulary changes, the CE's status changes with it. Users who write automation against CE conditions are unknowingly coupling to COS implementation details. A clean abstraction boundary requires the CE to **translate** COS state into CE-native conditions with stable, CE-owned semantics.

### CE status violates Kubernetes API conventions

`Progressing=True, Reason=Succeeded` means "finished progressing," which contradicts the natural reading of `Progressing=True` as "active work is happening." This violates the [Kubernetes API conventions for conditions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties). The impact extends beyond confusion — it breaks standard Kubernetes patterns that users and tooling depend on:

- **`kubectl wait`**: `kubectl wait --for=condition=Progressing=False` never succeeds because the terminal state is `Progressing=True/Succeeded`. Users cannot write correct CI/CD gates against CE status.
- **GitOps health checks**: ArgoCD, Flux, and other GitOps controllers use conditions to determine resource health. `Progressing=True` is universally interpreted as "not yet converged," causing GitOps tools to report healthy extensions as perpetually progressing.
- **Monitoring and alerting**: Prometheus alerting rules and kube-state-metrics dashboards that key on `Progressing=True` as a signal for active work will never clear, generating false positives or forcing users to write OLM-specific carve-outs.

### CE status is insufficient for troubleshooting

The mirrored COS conditions (`Available` and `Progressing`) do provide health and error visibility on the CE — users can see probe failures and rollout errors without inspecting the COS directly. However, the CE status surface still lacks key signals for common troubleshooting and operational scenarios:

1. **No CE-native health signal**: The COS `Available` condition is mirrored onto the CE, providing a proxy for health — but this is a leaked COS condition with COS-specific semantics (`ProbesSucceeded`, `ProbeFailure`), not a CE-native health signal. Its meaning is tied to COS internals and could change if the COS implementation changes. Meanwhile, `Installed=True` means "a bundle was installed," not "the managed resources are currently healthy." The CE needs its own health condition with stable, CE-owned semantics.

2. **No operation visibility**: During an upgrade or reconfiguration, users cannot see what is happening from the CE status alone. There is no indication of what kind of operation is in progress (install, upgrade, or reconfiguration), and the target version is only visible in COS annotations. Consider an SRE triaging a fleet of extensions:

    ```
    $ kubectl get clusterextensions
    NAME              INSTALLED BUNDLE                     VERSION   INSTALLED   PROGRESSING   AGE
    cert-manager      quay.io/example/cert-manager:v1.14   1.14.0    True        True          30d
    my-operator       quay.io/example/my-operator:v1.0     1.0.0     True        True          5d
    broken-operator                                                  False       True          2h
    ```

    All three show `PROGRESSING=True`. Which is healthy and idle? Which is mid-upgrade? Which is stuck? The SRE cannot tell — every state looks the same. There is no target version, no operation type, and no distinction between "upgrading to v2.0.0," "reconfiguring with new settings," and "happily running, nothing happening." Answering any of these questions requires inspecting COS objects or running `kubectl describe` on each extension individually.

3. **No phase visibility on COS**: When a COS rollout stalls, users get a `ProbeFailure` message but cannot see which phase is stuck, which phases completed, or the overall progress through the phased rollout. For example, a `ProbeFailure` message like `"Object Deployment.apps/v1 my-ns/my-deploy: updatedReplicas (0) != replicas (3)"` tells the user *what* is failing but not *where in the rollout* it failed — were CRDs applied? Did RBAC get set up? Is this the first phase or the last? Answering these questions requires inspecting individual managed objects one by one.

4. **Print columns don't surface what matters**: The CE print columns show `Installed Bundle` (a full image reference — rarely needed at a glance) and the `Installed` condition (less actionable than a health signal). The most common triage question — "is anything broken and does it need my attention?" — cannot be answered from the default `kubectl get` output. Compare the current output above with what this RFC proposes:

    ```
    $ kubectl get clusterextensions
    NAME              VERSION   READY   PROGRESSING   STATUS      OPERATION     TARGET   AGE
    cert-manager      1.14.0    True    False         Succeeded                          30d
    my-operator       1.0.0     False   True          Deploying   Upgrade       2.0.0    5d
    broken-operator   <none>    False   False         Blocked     Install       1.0.0    2h
    reconfigured-op   1.0.0     False   True          Deploying   Reconfigure   1.0.0    10d
    ```

    At a glance: `cert-manager` is healthy, `my-operator` is mid-upgrade to 2.0.0, `broken-operator` is stuck and needs attention, and `reconfigured-op` is applying a configuration change (same version, different settings). No `kubectl describe`, no COS inspection required.

5. **No Kubernetes Events**: Neither the CE nor the COS controller emits Kubernetes Events. Conditions capture the current state, but they don't capture *when* things happened — there is no time-series trail of rollout progress, errors, or state transitions. An SRE running `kubectl describe clusterextension my-operator` sees conditions but no Events section. This means there is no way to answer "when did this start failing?" or "what changed 10 minutes ago?" without correlating controller logs. Events are a standard Kubernetes observability mechanism — `kubectl get events`, `kubectl describe`, and monitoring tools all consume them — and their absence leaves a gap in the operational workflow.

# **Approach**

## Overview

This RFC proposes changes across both the CE and COS APIs to establish a clean abstraction boundary between the user-facing CE and the internal COS, while ensuring the CE status surface works correctly with standard Kubernetes tooling and workflows.

**Guiding principles**:

1. **COS is an implementation detail.** Users should be able to understand, diagnose, and act on their CE status without ever looking at a COS. COS inspection is reserved for extreme debugging scenarios.
2. **CE must not be a leaky abstraction.** The CE translates COS state into CE-native conditions with CE-owned semantics. Changes to COS internals should not change the CE's API contract.
3. **Follow Kubernetes conventions.** CE conditions must behave as the [Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties) specify, so that `kubectl wait`, GitOps health checks, and standard monitoring integrations work without OLM-specific workarounds.
4. **Design for how operators are managed at scale.** The status surface must support the full operational lifecycle: `kubectl` triage at a glance, `kubectl wait` for CI/CD gates, GitOps health assessment, Prometheus-based alerting, and SRE runbook-driven incident response.

## Part 1: ClusterExtension Status Changes

### 1.1 Fix Progressing Condition Semantics (Bug Fix)

**Current (broken)**:

*CE-native (set by the CE controller directly):*
- `Progressing=True, Reason=Succeeded` → "finished progressing" (contradicts Progressing=True semantics)
- `Progressing=True, Reason=Retrying` → "retrying after error"
- `Progressing=True, Reason=RollingOut` → "active rollout" (Helm applier path)
- `Progressing=False, Reason=Blocked` → "terminal error"
- `Progressing=False, Reason=InvalidConfiguration` → "invalid configuration"

*Mirrored from COS (boxcutter applier path — copied directly onto CE):*
- `Progressing=True, Reason=Succeeded` → "COS finished rolling out" (same bug, from COS)
- `Progressing=True, Reason=RollingOut` → "COS active rollout"
- `Progressing=True, Reason=Retrying` → "COS transient error"
- `Progressing=False, Reason=Blocked` → "COS terminal error"
- `Progressing=False, Reason=ProgressDeadlineExceeded` → "COS deadline exceeded"
- `Progressing=False, Reason=Archived` → "COS revision archived"

**Proposed (fixed)**:
- `Progressing=False, Reason=Succeeded` → "finished, not progressing anymore"
- `Progressing=True, Reason=Deploying` → "active rollout, no issues" (unchanged)
- `Progressing=True, Reason=ProbeFailure` → "rollout in progress but probes failing"
- `Progressing=True, Reason=ResolutionFailed` → "bundle resolution failed, retrying"
- `Progressing=True, Reason=ImagePullFailed` → "image pull failed, retrying"
- `Progressing=True, Reason=PreflightFailed` → "CE preflight check failed (e.g., ServiceAccount not found), retrying"
- `Progressing=True, Reason=AuthorizationFailed` → "RBAC insufficient, retrying"
- `Progressing=False, Reason=UnsupportedContent` → "bundle content unsupported, requires different version or OLM feature support"
- `Progressing=True, Reason=SafetyCheckFailed` → "CRD safety or other preflight check failed, retrying"
- `Progressing=True, Reason=Retrying` → "COS-level transient error, retrying"
- `Progressing=False, Reason=Blocked` → "terminal error" (unchanged)
- `Progressing=False, Reason=InvalidConfiguration` → "invalid configuration, requires manual fix"
- `Progressing=False, Reason=ProgressDeadlineExceeded` → "timed out"

This aligns with the Kubernetes convention: `Progressing=True` means active work is happening. This change can be treated as a bug fix since the current behavior contradicts the documented convention.

### 1.2 Add CE-Native Ready Condition

Today, the CE's only health-like signal is the COS `Available` condition mirrored directly onto the CE (see §1.5). This is a leaked COS condition with COS-specific reasons (`ProbesSucceeded`, `ProbeFailure`) — not a CE-native signal. This section proposes replacing it with a CE-owned `Ready` condition that the CE controller derives from COS state using CE-native semantics. The COS-level rename from `Available` to `Ready` is a separate change covered in §2.1.

**Why `Ready` over `Available`**: `Ready` is the [Kubernetes API conventions](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties) recommended top-level summary condition for long-running resources. It is an oscillating, point-in-time signal — "the object was believed to be fully operational at the time it was last probed." This matches what OLM can verify: managed resources are on-cluster and passing their probes.

`Available` carries different semantics in Kubernetes. The only core resource that uses it is Deployment, where `Available` means "at least the minimum required replicas have been `Ready` for at least `minReadySeconds`" — a temporal stability guarantee, not a point-in-time health check. Cluster API follows the same pattern: Machine has both `Ready` (healthy now) and `Available` (ready for `MinReadySeconds`); higher-level resources like MachineDeployment and Cluster use only `Available` to express minimum operational capacity during rolling operations.

OLM has no concept of `minReadySeconds` or sustained health duration, so using `Available` would be semantically misleading — it would imply a stability guarantee that doesn't exist. `Ready` accurately conveys what the condition reports: "are the managed resources healthy right now?"

This choice aligns with ecosystem convention. Across the Kubernetes ecosystem, `Ready` is the standard top-level health condition for CRDs:

| Resource | Condition | Semantics | Duration? |
|----------|-----------|-----------|-----------|
| Pod | `Ready` | Containers + readiness gates healthy | No |
| Node | `Ready` | Kubelet healthy, can accept pods | No |
| Deployment | `Available` | Min replicas ready for `minReadySeconds` | **Yes** |
| Knative (Service, Revision, etc.) | `Ready` | Top-level "happy state," aggregated from children | No |
| Crossplane (MR, XR, Claim) | `Ready` | Resource appears ready to use (cumulative) | No |
| CAPI Machine | `Ready` | Can host workloads (point-in-time) | No |
| CAPI Machine | `Available` | Ready for `MinReadySeconds` (stability) | **Yes** |
| cert-manager (Certificate, Issuer) | `Ready` | Able to function / cert obtained | No |

ArgoCD's [health assessment](https://argo-cd.readthedocs.io/en/latest/operator-manual/health/) treats `Ready=True` on CRDs as the primary signal for "Healthy." Choosing `Ready` ensures CE health status integrates with GitOps tooling out of the box.

Finally, `Available` is reserved for potential future use as a stability signal. If OLM later adds a concept of sustained health (e.g., "the extension has been Ready for a configured duration"), `Available` would be the natural condition for that — following the Deployment and CAPI pattern where `Available` = `Ready` + temporal stability.

| Status | Reason | Meaning |
|--------|--------|---------|
| True | `Succeeded` | Resources healthy, all probes pass |
| False | `Absent` | No bundle installed yet (nothing deployed to be healthy) |
| False | `ProbeFailure` | Specific probe failure on managed resources |
| False | `Deploying` | Rollout in progress, objects in transition — probes not yet passing |
| Unknown | `Pending` | Initial state before first reconcile |

**How Ready is derived**:

Ready answers ONE question: **"are the managed resources currently healthy?"** It does not carry pipeline error detail (resolution failures, config errors, pull errors) — that is `Progressing`'s job. This follows the Kubernetes Deployment pattern where `Available` and `Progressing` are orthogonal signals that don't duplicate each other's information.

- **When a COS revision is rolling out** (latest active COS without `succeededAt`): `Ready` reflects that COS's probe state. If it's still rolling out and probes haven't been evaluated, `Ready=False/Deploying`. If probes are failing, `Ready=False/ProbeFailure`. This means **Ready drops during normal upgrades** — it tracks the actual on-cluster state, not a cached view from the old revision.
- **When only the installed revision exists** (no rollout in progress): `Ready` reflects the installed COS's probe state. If probes pass, `Ready=True/Succeeded`. If probes fail (e.g., a managed resource was deleted or is unhealthy — drift recovery), `Ready=False/ProbeFailure`. See §1.3 for how drift recovery interacts with `Progressing` and `operation`.
- **When a pre-COS error occurs** (resolution failure, pull failure, validation error — no new COS is created): If an installed revision exists, `Ready` stays based on that installed revision's probe state (the error didn't create a new COS, so the on-cluster state hasn't changed). If no installed revision exists, `Ready=False/Absent` — there's simply nothing deployed. The specific error goes in `Progressing`, not `Ready`.
- **When nothing is installed and no error has occurred yet**: `Ready=Unknown/Pending` (brief initial state before first reconcile).

**Why Ready drops during upgrades**: During a multi-revision transition, objects are adopted by the new COS phase by phase. The old COS reports handed-off objects as `Progressed` (complete) even though the new COS's version of those objects might be failing probes. Deriving Ready from the old COS would be misleading — it would say "healthy" when the actual on-cluster objects are in a mixed or broken state. By tracking the latest active COS's probe state, Ready reflects what is actually deployed.

**Ready vs Installed**: These answer different questions:
- `Installed` → "Has a bundle been successfully installed?" (static fact, based on whether any COS has completed rollout)
- `Ready` → "Are the managed resources currently healthy right now?" (dynamic health check, tracks the latest active COS's probe state)

You can have `Installed=True, Ready=False/Deploying` during a normal upgrade — the old version was installed but objects are being replaced, and the new revision hasn't completed. Once the upgrade finishes, Ready returns to True.

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

**When nil (no rollout information)**: `status.operation` is nil in three cases: (1) the extension is in steady state (`Progressing=False/Succeeded`), (2) errors that occur *before* bundle resolution completes (e.g., bundle not found, catalog unavailable) — the controller doesn't yet know the target bundle, so it cannot populate the field, and (3) drift recovery — the installed COS detects a missing or unhealthy managed resource and self-heals within the current revision.

**Drift recovery** (case 3): When a managed resource is deleted or becomes unhealthy, the installed COS re-creates it and waits for probes to pass. During this brief window, the CE controller sets `Ready=False/ProbeFailure` and `Progressing=True/ProbeFailure` — signaling that the system is actively self-healing. No `operation` is set because drift recovery is not a revision-level rollout (no new COS is created, no spec change occurred). The absence of `operation` distinguishes drift recovery from a stuck upgrade (§3.14), where `operation` shows the target version. Once the COS self-heals and probes pass, both conditions return to steady state (`Ready=True/Succeeded`, `Progressing=False/Succeeded`).

**How the type is determined by the controller**:
- `Install`: `status.install` is nil (no previous installation)
- `Upgrade`: `status.install` is non-nil and the target bundle version differs from the installed version
- `Reconfigure`: `status.install` is non-nil and the target bundle version matches the installed version

### 1.4 Remove activeRevisions (Experimental)

The `status.activeRevisions` field is experimental and exposes COS implementation details to CE users. With the addition of `Ready` (health signal) and `status.operation` (upgrade visibility), users no longer need to inspect revision-level detail on the CE.

This field is removed. Users who need revision-level detail can inspect COS resources directly.

### 1.5 Stop Mirroring COS Conditions — Translate Instead

The CE controller currently mirrors COS `Available` and `Progressing` conditions directly onto the CE, including COS-specific reasons like `ProbesSucceeded` and `Deploying`.

**Proposed**: The CE controller reads COS state but translates it into CE-native conditions (`Ready`, `Progressing`, `Installed`) with CE-native reasons. No COS conditions appear on the CE.

**Critical: CE must surface COS blocking errors.** When the latest rolling-out COS has a terminal error (`Progressing=False/Blocked`, `Progressing=False/ProgressDeadlineExceeded`), the CE controller must detect this and reflect it in the CE's `Progressing` condition. Without this, the CE would show `Progressing=True/Deploying` while the COS is terminally stuck — leaving the user unable to diagnose the problem from the CE alone.

Translation rules for the CE `Progressing` condition from COS state:
- COS `Progressing=True/RollingOut` AND COS `Ready=False/ProbeFailure` → CE `Progressing=True/ProbeFailure` with the probe failure detail. This distinguishes a stuck rollout from a healthy one.
- COS `Progressing=True/RollingOut` AND COS `Ready=False/RollingOut` → CE `Progressing=True/Deploying` (normal progress, no issues).
- COS `Progressing=True/RollingOut` AND COS `Ready=Unknown/Reconciling` → CE `Progressing=True/Deploying` (momentary transient state during normal progress; the COS is still making forward progress).
- COS `Progressing=True/Retrying` → CE `Progressing=True/Retrying` with the COS error message
- COS `Progressing=True/ObjectCollisionDetected` → CE `Progressing=True/Retrying` with the COS collision detail
- COS `Progressing=True/ValidationFailed` → CE `Progressing=True/Retrying` with the COS validation error
- COS `Progressing=False/Blocked` → CE `Progressing=False/Blocked` with the COS error message
- COS `Progressing=False/ProgressDeadlineExceeded` → CE `Progressing=False/ProgressDeadlineExceeded` with the COS message

Translation rules for the CE `Ready` condition from COS state:
- When the latest COS has `Ready=True/ProbesSucceeded` → CE `Ready=True/Succeeded`
- When the latest COS has `Ready=False/ProbeFailure` → CE `Ready=False/ProbeFailure` with the probe failure detail
- When the latest COS has `Ready=False/RollingOut` → CE `Ready=False/Deploying`
- When the latest COS has `Ready=Unknown/Reconciling` (pre-deployment failure — e.g., object collision, validation error, secret immutability): CE `Ready` stays based on the **installed** revision's probe state. If an installed revision exists and its probes pass, `Ready=True/Succeeded`. If no installed revision exists, `Ready=False/Absent`. Rationale: the latest COS failed before deploying any objects, so the actual on-cluster state has not changed from the installed revision.

This ensures the RFC's guiding principle holds: users can understand, diagnose, and act on their CE status without ever looking at a COS.

### 1.6 Updated CE Print Columns

**Current**: `Installed Bundle`, `Version`, `Installed`, `Progressing`, `Age`

```
$ kubectl get clusterextensions
NAME              INSTALLED BUNDLE                     VERSION   INSTALLED   PROGRESSING   AGE
cert-manager      quay.io/example/cert-manager:v1.14   1.14.0    True        True          30d
my-operator       quay.io/example/my-operator:v1.0     1.0.0     True        True          5d
broken-operator                                                  False       True          2h
```

**Proposed**: `Version`, `Ready`, `Progressing`, `Status`, `Operation`, `Target`, `Age`

| Column | JSONPath | Rationale |
|--------|---------|-----------|
| Version | `.status.install.bundle.version` | What version is installed — immediately identifies the extension alongside its name |
| Ready | `.status.conditions[?(@.type=='Ready')].status` | Primary health signal |
| Progressing | `.status.conditions[?(@.type=='Progressing')].status` | Is something actively happening? Standard Kubernetes boolean |
| Status | `.status.conditions[?(@.type=='Progressing')].reason` | **Why** — the Progressing condition's reason, displayed as a user-facing status (following the Pod `STATUS` column convention). Each value identifies a specific state: `Succeeded`, `Deploying`, `ProbeFailure`, `ResolutionFailed`, `ImagePullFailed`, `PreflightFailed`, `AuthorizationFailed`, `UnsupportedContent`, `SafetyCheckFailed`, `Retrying`, `Blocked`, `InvalidConfiguration`, `ProgressDeadlineExceeded` |
| Operation | `.status.operation.type` | What kind of rollout is in progress (Install/Upgrade/Reconfigure). Empty in steady state |
| Target | `.status.operation.bundle.version` | What version is being rolled out to. Empty in steady state |
| Message | `.status.conditions[?(@.type=='Progressing')].message` | **Wide only** (priority=1, shown with `-o wide`). The Progressing condition's message — gives the specific error detail inline without requiring `kubectl describe` |
| Age | `.metadata.creationTimestamp` | Standard — always last per kubectl convention |

**Column grouping**: The columns are ordered for left-to-right triage. **Identity** (NAME + Version) tells you what this is. **Health** (Ready + Progressing + Status) tells you if it's okay and why. **Activity** (Operation + Target) tells you what's happening — both empty in steady state, keeping the common view clean. The `Message` column is hidden by default and shown with `-o wide` — it provides the full error detail for SREs who need it without cluttering the default table.

**Triage at a glance**: `Succeeded` = all good. `Deploying` = normal upgrade, wait. Any `*Failed` reason = specific retryable problem (use `-o wide` for detail). `Retrying` = COS-level transient error. `Blocked/InvalidConfiguration/UnsupportedContent/ProgressDeadlineExceeded` = **needs attention, won't self-resolve**.

Example (default):

```
$ kubectl get clusterextensions
NAME              VERSION   READY   PROGRESSING   STATUS                OPERATION   TARGET   AGE
cert-manager      1.14.0    True    False         Succeeded                                  30d
my-operator       1.0.0     False   True          Deploying             Upgrade     2.0.0    5d
broken-operator   <none>    False   False         Blocked               Install     1.0.0    2h
pull-fail         <none>    False   True          ImagePullFailed       Install     1.0.0    5m
no-rbac           1.0.0     True    True          AuthorizationFailed   Upgrade     2.0.0    5d
```

Example (wide — includes MESSAGE):

```
$ kubectl get clusterextensions -o wide
NAME              VERSION   READY   PROGRESSING   STATUS                OPERATION   TARGET   MESSAGE                                                                        AGE
cert-manager      1.14.0    True    False         Succeeded                                  Desired state reached                                                          30d
broken-operator   <none>    False   False         Blocked               Install     1.0.0    error parsing image reference "!!!invalid": invalid reference format           2h
pull-fail         <none>    False   True          ImagePullFailed       Install     1.0.0    error copying image: authentication required                                   5m
no-rbac           1.0.0     True    True          AuthorizationFailed   Upgrade     2.0.0    pre-authorization failed: SA requires permissions: [create deployments.apps]   5d
```

### 1.7 Complete CE Condition Summary

| Condition | Status=True | Status=False | Status=Unknown |
|-----------|------------|-------------|----------------|
| **Installed** | `Succeeded` — a bundle is installed | `Absent` — no bundle installed | — |
| **Ready** | `Succeeded` — resources healthy, probes pass | `Absent` — no bundle installed (nothing deployed); `ProbeFailure` — specific probe failure on managed resources; `Deploying` — objects in transition, probes not yet passing | `Pending` — initial state before first reconcile |
| **Progressing** | `Deploying` — active deployment, no issues; `ProbeFailure` — deployment active, probes failing; `ResolutionFailed` — bundle not found; `ImagePullFailed` — image pull error; `PreflightFailed` — CE preflight check failed (e.g., ServiceAccount not found); `AuthorizationFailed` — RBAC insufficient; `SafetyCheckFailed` — safety check failed; `Retrying` — COS-level transient error | `Succeeded` — done; `Blocked` — terminal error; `InvalidConfiguration` — bad config; `UnsupportedContent` — bundle content unsupported; `ProgressDeadlineExceeded` — timed out | — |
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

### 1.9 CE-Level Progress Deadline

**Problem**: Several `Progressing=True` reasons — `AuthorizationFailed`, `SafetyCheckFailed`, `PreflightFailed` — represent errors that will never self-resolve without human intervention. Yet `Progressing=True` signals "the system is working on it, wait." This is technically accurate (the controller *is* retrying), but misleading from an operational perspective: the retry will never succeed until someone fixes the underlying issue. The result is that `Progressing=True` alone cannot distinguish "normal rollout in progress" from "stuck on an error that needs a human."

The COS already solves this for rollout-phase errors via `spec.progressDeadlineMinutes` — after the deadline, `Progressing=True/RollingOut` transitions to `Progressing=False/ProgressDeadlineExceeded`. But pre-COS errors (resolution, image pull, RBAC, validation) happen before a COS exists, so no COS deadline can fire.

**Proposed**: Add a CE-level progress deadline that covers the entire pipeline — from the moment a spec change is observed until a successful rollout completes. This closes the gap for pre-COS errors and provides a single, universal "needs attention" signal: `Progressing=False` with a non-`Succeeded` reason.

```go
type ClusterExtensionSpec struct {
    // ... existing fields ...

    // progressDeadlineMinutes specifies the maximum time in minutes that the
    // controller will retry before marking the extension as not progressing.
    // The deadline covers the entire pipeline: resolution, image pull,
    // validation, RBAC checks, and COS rollout. When the deadline is exceeded,
    // the Progressing condition transitions to False/ProgressDeadlineExceeded.
    // The controller continues retrying in the background — a successful
    // retry resets the condition.
    //
    // When set, this value is also used to configure the COS-level progress
    // deadline on newly created revisions.
    //
    // Default: 30. Set to 0 to disable.
    // +optional
    // +kubebuilder:default=30
    // +kubebuilder:validation:Minimum=0
    ProgressDeadlineMinutes *int32 `json:"progressDeadlineMinutes,omitempty"`
}
```

**Deadline lifecycle**:

1. **Starts** when a new `observedGeneration` is detected (the user changed the CE spec) and `Progressing` transitions to `True`.
2. **Ticks** while `Progressing=True` — counting time spent in any retrying state (resolution, pull, RBAC, COS rollout, etc.).
3. **Fires** when elapsed time exceeds `progressDeadlineMinutes`. The CE's `Progressing` condition transitions from `True/<reason>` to `False/ProgressDeadlineExceeded`. The last error message is preserved in the condition message (e.g., "Progress deadline exceeded after 30 minutes. Last error: pre-authorization failed: service account requires permissions [create deployments.apps]").
4. **Resets** when `Progressing` transitions to `False/Succeeded` (rollout completed) or the spec changes again (new `observedGeneration` restarts the deadline).
5. **Recovers**: If the underlying issue is fixed (e.g., RBAC granted) while in `ProgressDeadlineExceeded`, the next successful reconcile transitions back to `Progressing=False/Succeeded`. The deadline is not a permanent tombstone.

**Interaction with COS deadline**: When the CE controller creates a COS, it sets the COS's `spec.progressDeadlineMinutes` to the remaining time from the CE deadline. This ensures a single pipeline-wide budget: if resolution took 10 minutes of a 30-minute deadline, the COS gets 20 minutes for its rollout. The COS deadline is an implementation detail derived from the CE deadline.

**Why the controller continues retrying after the deadline**: The deadline changes the *signal*, not the *behavior*. The controller continues retrying because the issue may self-resolve (e.g., catalog update, registry recovery). But the signal to the user changes from "wait" (`Progressing=True`) to "needs attention" (`Progressing=False/ProgressDeadlineExceeded`). If a retry succeeds after the deadline, the condition recovers to `Progressing=False/Succeeded`.

**Default value**: 30 minutes. Inspired by the Kubernetes Deployment `progressDeadlineSeconds` default (600s = 10 minutes), scaled up 3x to account for the broader OLM pipeline (resolution + pull + validation + rollout). Set to 0 to disable deadline enforcement entirely.

**Impact on alerting**: With the CE deadline, a single alert rule covers all failure modes:

```
alert: Progressing=False AND reason NOT IN (Succeeded, Archived)
```

This fires for `Blocked`, `InvalidConfiguration`, `ProgressDeadlineExceeded` — all states that need human attention. No need to enumerate individual `*Failed` reasons or add duration-based heuristics.

## Part 2: ClusterObjectSet Status Changes

The COS currently has three conditions and three print columns:

**Current COS Conditions (3)**:

| Condition | Status | Reason | Meaning |
|-----------|--------|--------|---------|
| **Available** | True | `ProbesSucceeded` | All managed objects pass probes |
| | False | `ProbeFailure` | One or more objects failing probes |
| | False | `RollingOut` | Rollout in progress, probes not yet evaluated |
| | Unknown | `Reconciling` | Transient error during reconciliation |
| | Unknown | `Archived` | Revision archived, objects torn down |
| **Progressing** | True | `Succeeded` | Rollout complete (same bug as CE — contradicts Progressing=True) |
| | True | `RollingOut` | Active rollout in progress |
| | True | `Retrying` | Transient error (collisions, validation, watch setup, etc.) |
| | False | `Blocked` | Terminal error (secret immutability, content digest mismatch) |
| | False | `Archived` | Revision archived |
| | False | `ProgressDeadlineExceeded` | Rollout exceeded deadline |
| **Succeeded** | True | `Succeeded` | Latch — set once when rollout completes, never cleared |

**Current COS Print Columns**:

```
$ kubectl get clusterobjectsets
NAME            AVAILABLE   PROGRESSING   AGE
my-operator-1   True        True          5d
my-operator-2   False       True          30s
```

No revision number, no status reason — the user can see two COS objects exist but can't tell which is the old vs new revision or why one is unavailable.

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
| **Ready** | `ProbesSucceeded` — all managed objects pass probes | `ProbeFailure` — one or more probe failures; `RollingOut` — rollout not yet complete | `Reconciling` — transient error |

**Note:** The `Ready` condition is not set on archived revisions — their objects have been torn down, so readiness is not applicable. The `Archived` reason on `Progressing` already conveys the lifecycle state.
| **Progressing** | `RollingOut` — active rollout; `ObjectCollisionDetected` — object ownership conflict; `ValidationFailed` — preflight/dry-run failure; `Retrying` — other transient error | `Succeeded` — rollout complete; `Blocked` — terminal error; `Archived` — revision archived; `ProgressDeadlineExceeded` — deadline exceeded | — |

**Key changes**:
- `Progressing=True, Reason=Succeeded` (the same bug as CE) is fixed to `Progressing=False, Reason=Succeeded`.
- The `Ready` condition is always set on first reconcile for **active** revisions, even if probes haven't been evaluated. Pre-phase errors set `Ready=Unknown/Reconciling` rather than leaving the condition absent. Archived revisions do NOT set `Ready` — their objects have been torn down, so readiness is not applicable.

### 2.5 Updated COS Print Columns

**Current**: `Available`, `Progressing`, `Age`

```
$ kubectl get clusterobjectsets
NAME            AVAILABLE   PROGRESSING   AGE
my-operator-1   True        True          30d
my-operator-2   False       True          5m
```

**Proposed**: `Revision`, `Ready`, `Progressing`, `Status`, `Age` (+ `Message` with `-o wide`)

| Column | JSONPath | Rationale |
|--------|---------|-----------|
| Revision | `.spec.revision` | Which revision number — essential for debugging multi-revision scenarios |
| Ready | `.status.conditions[?(@.type=='Ready')].status` | Health signal (renamed from Available) |
| Progressing | `.status.conditions[?(@.type=='Progressing')].status` | Is active work happening |
| Status | `.status.conditions[?(@.type=='Progressing')].reason` | Why — `Archived` in the status column replaces the need for a separate Lifecycle column. Matches the CE pattern for consistent triage |
| Message | `.status.conditions[?(@.type=='Progressing')].message` | **Wide only** (priority=1, shown with `-o wide`). Specific error detail for debugging |
| Age | `.metadata.creationTimestamp` | Standard — always last per kubectl convention |

The `Lifecycle` column is dropped because the `Status` column already shows `Archived` for archived revisions — any other reason implies Active.

Example with multiple revisions including archived:

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY    PROGRESSING   STATUS      AGE
my-operator-1   1          <none>   False         Archived    30d
my-operator-2   2          <none>   False         Archived    5d
my-operator-3   3          True     False         Succeeded   1d
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
    //
    // The Ready condition is not set on archived revisions — readiness is not applicable
    // when objects have been torn down.
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

**Current output** (what users see today):

```
$ kubectl get clusterextensions
NAME          INSTALLED BUNDLE                         VERSION   INSTALLED   PROGRESSING   AGE
my-operator   quay.io/example/my-operator:v1.0.0       1.0.0     True        True          5d
```

`PROGRESSING=True` even though nothing is happening — this is the `Progressing=True/Succeeded` bug. Every healthy, idle extension looks like it's actively progressing.

**Proposed output**:

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS      OPERATION   TARGET   AGE
my-operator   1.0.0     True    False         Succeeded                        5d
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

### 3.1a Drift Recovery: Managed Resource Deleted

The extension is installed and was healthy, but someone (or another controller) deleted a managed resource (e.g., a Deployment). The COS detects the missing resource via probe failure and re-creates it automatically. During the brief recovery window:

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS         OPERATION   TARGET   AGE
my-operator   1.0.0     False   True          ProbeFailure                        5d
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
    reason: ProbeFailure
    message: "Object Deployment.apps/v1 my-ns/my-deploy: object not found"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ProbeFailure
    message: "Object Deployment.apps/v1 my-ns/my-deploy: object not found"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install:
    bundle:
      name: my-operator
      version: 1.0.0
  operation: null
```

**Key UX point**: `Ready=False/ProbeFailure` with `Progressing=True/ProbeFailure` — the COS is actively re-creating the missing resource and waiting for probes to pass. No `operation` is set because drift recovery is not a revision-level rollout — it's the COS self-healing within the current revision (no new COS is created). The absence of `operation` distinguishes drift recovery from a stuck upgrade (§3.14), where `operation` shows the target version.

Once the COS re-applies the resource and probes pass, both conditions return to their steady state: `Ready=True/Succeeded` and `Progressing=False/Succeeded`.

**User action**: Typically none — the COS self-heals within seconds. If the resource keeps being deleted (e.g., by another controller), investigate the external cause. If `Ready` does not recover, check the COS for details.

---

### 3.1b Happy Path: Initial State (Pre-Reconcile)

A ClusterExtension has just been created. The controller has not yet reconciled it — this is the brief initial state before any work begins.

```
$ kubectl get clusterextensions
NAME          VERSION   READY     PROGRESSING   STATUS      OPERATION   TARGET   AGE
my-operator   <none>    Unknown   True          Deploying                        2s
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
    status: "Unknown"
    reason: Pending
    message: "Waiting for initial reconciliation"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: Deploying
    message: "Extension is being processed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation: null
```

**Key UX point**: `Ready=Unknown/Pending` is a transient state that lasts only until the first reconcile completes (typically seconds). No COS exists yet. Once the controller processes the CE, it transitions to one of the other states (§3.2 if install begins, §3.5 if resolution fails, etc.).

**User action**: Wait. This state resolves within seconds.

---

### 3.2 Happy Path: First Install In Progress

A new ClusterExtension is being installed for the first time. The COS is rolling out phases sequentially.

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS      OPERATION   TARGET   AGE
my-operator   <none>    False   True          Deploying   Install     1.0.0    30s
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
    reason: Deploying
    message: "Managed resources are being updated"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: Deploying
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
NAME            REVISION   READY   PROGRESSING   STATUS       AGE
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

**Current output** (what users see today):

```
$ kubectl get clusterextensions
NAME          INSTALLED BUNDLE                         VERSION   INSTALLED   PROGRESSING   AGE
my-operator   quay.io/example/my-operator:v1.0.0       1.0.0     True        True          5d
```

Indistinguishable from §3.1 (steady state) — same `PROGRESSING=True`. No indication an upgrade is happening, no target version, no health signal. The user must inspect the COS to understand what's going on.

**Proposed output**:

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS      OPERATION   TARGET   AGE
my-operator   1.0.0     False   True          Deploying   Upgrade     2.0.0    5d
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
    reason: Deploying
    message: "Managed resources are being updated"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: Deploying
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

**Current COS output**:

```
$ kubectl get clusterobjectsets
NAME            AVAILABLE   PROGRESSING   AGE
my-operator-1   True        True          5d
my-operator-2   False       True          30s
```

No revision number, no status reason — the user can see two COS objects exist but can't tell which is the old vs new revision or why one is unavailable.

**Proposed COS output**:

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   STATUS       AGE
my-operator-1   1          True    False         Succeeded    5d
my-operator-2   2          False   True          RollingOut   30s
```

**Key UX point**: `Ready=False` — the new revision's objects are still rolling out, so the on-cluster state is in transition. `Installed=True` confirms the previous version was installed. `Version=1.0.0` shows what was installed, `Operation=Upgrade` and `Target=2.0.0` show where it's headed. The COS table shows two active revisions. Once COS-2 completes, Ready returns to True.

**User action**: Wait. Monitor Progressing condition for progress.

---

### 3.3a Happy Path: Upgrade — Phase-Level View of Both Revisions

This shows the detailed phase-level state during the same upgrade from 3.3, viewed from both COS revisions. COS-2 is in the middle of its phased rollout, adopting objects from COS-1 phase by phase.

**Current COS output** (what users see today):

```
$ kubectl get clusterobjectsets
NAME            AVAILABLE   PROGRESSING   AGE
my-operator-1   True        True          5d
my-operator-2   False       True          2m
```

No revision number, no status reason, no phase detail. The user knows two COS objects exist and one is unavailable, but nothing else. To understand the rollout progress, they must `kubectl describe` each COS and manually inspect individual managed objects.

**Proposed COS output**:

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   STATUS       AGE
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
NAME          VERSION   READY   PROGRESSING   STATUS      OPERATION     TARGET   AGE
my-operator   1.0.0     False   True          Deploying   Reconfigure   1.0.0    5d
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
    reason: Deploying
    message: "Managed resources are being updated"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: Deploying
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
NAME            REVISION   READY   PROGRESSING   STATUS      AGE
my-operator-1   1          True    False         Succeeded   5d
my-operator-2   2          False   True          RollingOut  10s
```

**User action**: Wait. The reconfiguration is in progress.

---

### 3.4a Error: Reconfiguration Probe Failure

The user changed configuration (e.g., set an invalid resource limit, changed a config value) without changing the version. The new COS revision is rolling out but a Deployment's pods are failing probes.

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS         OPERATION     TARGET   AGE
my-operator   1.0.0     False   True          ProbeFailure   Reconfigure   1.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS         OPERATION     TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     False   True          ProbeFailure   Reconfigure   1.0.0    Rolling out configuration change for bundle my-operator v1.0.0: Obj...   5d
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
    reason: ProbeFailure
    message: "Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ProbeFailure
    message: "Rolling out configuration change for bundle my-operator v1.0.0: Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
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
NAME            REVISION   READY   PROGRESSING   STATUS       AGE
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
    message: "Revision 1.0.0 is rolling out."
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

**Key UX point**: `Operation=Reconfigure` and `VERSION=TARGET=1.0.0` make clear this is a configuration change gone wrong, not an upgrade. The user knows the version didn't change — the problem is the new configuration. Compare with §3.14 where `Operation=Upgrade` and different VERSION/TARGET values signal a version change.

**User action**: Investigate the probe failure. Revert the configuration change if the new settings are the cause (e.g., invalid resource limits causing OOMKills).

---

### 3.5 CE Error: Bundle Not Found (No Previous Install)

The user specifies a package name or version that doesn't exist in any catalog. Nothing was previously installed.

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS             OPERATION   TARGET   AGE
my-operator   <none>    False   True          ResolutionFailed                        2m
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS             OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   <none>    False   True          ResolutionFailed                        no bundles found for package \"my-operator\" matching version \">=9...   2m
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

**Key UX point**: `Ready=False/Absent` (nothing deployed), `Progressing=True/ResolutionFailed` with the specific resolution error. The user checks Progressing to understand what went wrong. No COS exists to inspect. No `operation` is set because resolution hasn't succeeded yet (we don't know the target bundle).

**User action**: Fix the package name, version constraint, or channel in the CE spec. Or add a catalog containing the desired package.

---

### 3.6 CE Error: Bundle Not Found (During Upgrade)

The user requests an upgrade to a version that doesn't exist, but the old version is still running.

**Current output** (what users see today):

```
$ kubectl get clusterextensions
NAME          INSTALLED BUNDLE                         VERSION   INSTALLED   PROGRESSING   AGE
my-operator   quay.io/example/my-operator:v1.0.0       1.0.0     True        True          5d
```

Identical to §3.1 (steady state) and §3.3 (upgrade in progress). The resolution error is completely invisible — the user has no idea that their version constraint doesn't match any available bundle.

**Proposed output**:

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS             OPERATION   TARGET   AGE
my-operator   1.0.0     True    True          ResolutionFailed                        5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS             OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    True          ResolutionFailed                        unable to upgrade to version >=99.0.0: no bundles found for package...   5d
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
NAME          VERSION   READY   PROGRESSING   STATUS                 OPERATION   TARGET   AGE
my-operator   1.0.0     True    False         InvalidConfiguration   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS                 OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    False         InvalidConfiguration   Upgrade     2.0.0    error for resolved bundle my-operator with version 2.0.0: invalid C...   5d
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
NAME          VERSION   READY   PROGRESSING   STATUS                 OPERATION   TARGET   AGE
my-operator   <none>    False   False         InvalidConfiguration   Install     1.0.0    2m
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS                 OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   <none>    False   False         InvalidConfiguration   Install     1.0.0    error for resolved bundle my-operator with version 1.0.0: invalid C...   2m
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
NAME          VERSION   READY   PROGRESSING   STATUS            OPERATION   TARGET   AGE
my-operator   <none>    False   True          ImagePullFailed   Install     1.0.0    5m
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS            OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   <none>    False   True          ImagePullFailed   Install     1.0.0    error for resolved bundle my-operator with version 1.0.0: error cop...   5m
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
NAME          VERSION   READY   PROGRESSING   STATUS    OPERATION   TARGET   AGE
my-operator   <none>    False   False         Blocked   Install     1.0.0    2m
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS    OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   <none>    False   False         Blocked   Install     1.0.0    error for resolved bundle my-operator with version 1.0.0: error par...   2m
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
NAME          VERSION   READY   PROGRESSING   STATUS            OPERATION   TARGET   AGE
my-operator   <none>    False   True          PreflightFailed   Install     1.0.0    1m
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS            OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   <none>    False   True          PreflightFailed   Install     1.0.0    operation cannot proceed due to the following validation error(s): ...   1m
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
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: PreflightFailed
    message: "operation cannot proceed due to the following validation error(s): service account \"my-sa\" not found in namespace \"my-ns\""
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  install: null
  operation:
    type: Install
    bundle:
      name: my-operator
      version: 1.0.0
```

**User action**: Create the ServiceAccount or fix the name/namespace in the CE spec.

---

### 3.12 CE Error: RBAC Insufficient (Pre-Authorization Failure)

The ServiceAccount exists but lacks RBAC permissions for the bundle's managed resources.

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS                OPERATION   TARGET   AGE
my-operator   1.0.0     True    True          AuthorizationFailed   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS                OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    True          AuthorizationFailed   Upgrade     2.0.0    error for resolved bundle my-operator with version 2.0.0: creating ...   5d
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
NAME          VERSION   READY   PROGRESSING   STATUS               OPERATION   TARGET   AGE
my-operator   1.0.0     True    False         UnsupportedContent   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS               OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    False         UnsupportedContent   Upgrade     2.0.0    error for resolved bundle my-operator with version 2.0.0: unsupport...   5d
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
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "False"
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

**Key UX point**: `Progressing=False` — this is terminal, not retrying. The resolved bundle's content won't change from retrying; the user must pick a different version or wait for OLM to add support. `Ready=True` — old version unaffected. No COS is created for the new version because the error occurs before revision creation. `operation` persists so the user can see what they were trying to roll out to.

**User action**: Use a different bundle version that doesn't use unsupported features, or wait for feature support.

---

### 3.14 COS Error: Probe Failure (Deployment Not Ready)

The COS revision is stuck because a Deployment's pods are not ready (e.g., image pull backoff, crash loop).

**Current output** (what users see today):

```
$ kubectl get clusterextensions
NAME          INSTALLED BUNDLE                         VERSION   INSTALLED   PROGRESSING   AGE
my-operator   quay.io/example/my-operator:v1.0.0       1.0.0     True        True          5d
```

The probe failure is completely invisible on the CE. The user must inspect the COS to discover that a Deployment is stuck. Again, identical to steady state (§3.1).

**Proposed output**:

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS         OPERATION   TARGET   AGE
my-operator   1.0.0     False   True          ProbeFailure   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS         OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     False   True          ProbeFailure   Upgrade     2.0.0    Rolling out bundle my-operator v2.0.0: Object Deployment.apps/v1 my...   5d
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

**Current COS output**:

```
$ kubectl get clusterobjectsets
NAME            AVAILABLE   PROGRESSING   AGE
my-operator-1   True        True          5d
my-operator-2   False       True          5m
```

No revision number, no reason why `my-operator-2` is unavailable, no phase detail.

**Proposed COS output**:

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY   PROGRESSING   STATUS       AGE
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

### 3.14a COS Error: Probe Failure (First Install)

Same as §3.14 but during a first-time installation — no previous version exists.

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS         OPERATION   TARGET   AGE
my-operator   <none>    False   True          ProbeFailure   Install     1.0.0    10m
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS         OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   <none>    False   True          ProbeFailure   Install     1.0.0    Rolling out bundle my-operator v1.0.0: Object Deployment.apps/v1 my...   10m
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
    reason: ProbeFailure
    message: "Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "True"
    reason: ProbeFailure
    message: "Rolling out bundle my-operator v1.0.0: Object Deployment.apps/v1 my-ns/my-deploy: \"status.updatedReplicas\" != \"status.replicas\" expected: 3 got: 0"
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
NAME            REVISION   READY   PROGRESSING   STATUS       AGE
my-operator-1   1          False   True          RollingOut   10m
```

```yaml
# COS my-operator-1 status (deep debugging)
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
    message: "Revision 1.0.0 is rolling out."
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

**Key UX point**: Compare with §3.14 (upgrade probe failure): `VERSION=<none>` (nothing installed yet), `Installed=False/Absent`, and only one COS revision exists. The user sees a starker picture — there's no fallback version running. `Ready=False/ProbeFailure` rather than `Ready=False/Absent` tells the user that resources *were* deployed but aren't healthy, which is more actionable than "nothing deployed."

**User action**: Investigate the Deployment (check pods, events, image availability). This is more urgent than §3.14 because there is no previously working version to fall back to.

---

### 3.15 COS Error: Object Collision

A managed object is already owned by another controller. The collision protection policy prevents adoption.

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS     OPERATION   TARGET   AGE
my-operator   1.0.0     True    True          Retrying   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS     OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    True          Retrying   Upgrade     2.0.0    Object collision in phase "roles": Deployment.apps/v1 my-ns/conflic...   5d

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   STATUS                    AGE
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

**Current output** (what users see today):

```
$ kubectl get clusterextensions
NAME          INSTALLED BUNDLE   VERSION   INSTALLED   PROGRESSING   AGE
my-operator                                False       False         5m
```

`PROGRESSING=False` — the one case where current output correctly signals a problem. But no reason *why* it's blocked, no target version, no health context. The user must describe the CE or inspect the COS to find the "secrets are not immutable" error.

**Proposed output**:

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS    OPERATION   TARGET   AGE
my-operator   <none>    False   False         Blocked   Install     1.0.0    5m
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS    OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   <none>    False   False         Blocked   Install     1.0.0    the following secrets are not immutable (referenced secrets must ha...   5m

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   STATUS    AGE
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
NAME          VERSION   READY   PROGRESSING   STATUS    OPERATION   TARGET   AGE
my-operator   1.0.0     True    False         Blocked   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS    OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    False         Blocked   Upgrade     2.0.0    resolved content of 1 phase(s) has changed: phase \"deploy\" (expec...   5d

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   STATUS      AGE
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
NAME          VERSION   READY   PROGRESSING   STATUS     OPERATION   TARGET   AGE
my-operator   1.0.0     True    True          Retrying   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS     OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    True          Retrying   Upgrade     2.0.0    revision validation error: dry-run apply rejected by webhook: admis...   5d

```
$ kubectl get clusterobjectsets
NAME            REVISION   READY     PROGRESSING   STATUS             AGE
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
NAME          VERSION   READY   PROGRESSING   STATUS                     OPERATION   TARGET   AGE
my-operator   <none>    False   False         ProgressDeadlineExceeded   Install     1.0.0    35m
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS                     OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   <none>    False   False         ProgressDeadlineExceeded   Install     1.0.0    Revision has not rolled out for 30 minute(s). Last status: Revision...   35m
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
NAME            REVISION   READY   PROGRESSING   STATUS                     AGE
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
NAME          VERSION   READY   PROGRESSING   STATUS                     OPERATION   TARGET   AGE
my-operator   1.0.0     False   False         ProgressDeadlineExceeded   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS                     OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     False   False         ProgressDeadlineExceeded   Upgrade     2.0.0    Revision has not rolled out for 30 minute(s). Last status: Revision...   5d
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
NAME            REVISION   READY   PROGRESSING   STATUS                     AGE
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
NAME          VERSION   READY   PROGRESSING   STATUS      OPERATION   TARGET   AGE
my-operator   1.0.0     True    False         Succeeded                        5d
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
NAME          VERSION   READY   PROGRESSING   STATUS              OPERATION   TARGET   AGE
my-operator   1.0.0     True    True          SafetyCheckFailed   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS              OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    True          SafetyCheckFailed   Upgrade     2.0.0    error for resolved bundle my-operator with version 2.0.0: CRD upgra...   5d
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
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 1
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "True"
    reason: SafetyCheckFailed
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
NAME          VERSION   READY   PROGRESSING   STATUS     OPERATION   TARGET   AGE
my-operator   1.0.0     True    True          Retrying                        30d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS     OPERATION   TARGET   MESSAGE                                                                  AGE
my-operator   1.0.0     True    True          Retrying                        migrating storage: listing ClusterObjectSets before attempting migr...   30d
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
NAME              VERSION   READY   PROGRESSING   STATUS      OPERATION   TARGET   AGE
cert-manager      1.14.0    True    False         Succeeded                        30d
my-operator       1.0.0     False   True          Deploying   Upgrade     2.0.0    5d
broken-operator   <none>    False   False         Blocked     Install     1.0.0    2h
deprecated-op     3.2.1     True    False         Succeeded                        90d
```

At a glance:
- `cert-manager`: Healthy, nothing happening — clean columns
- `my-operator`: NOT ready (upgrade in progress), upgrading to 2.0.0 — objects in transition
- `broken-operator`: NOT ready, NOT progressing, stuck on Install → 1.0.0 — needs attention
- `deprecated-op`: Healthy, nothing happening (check deprecation conditions for details)

**Distinguishing upgrade-in-progress from stuck**: Both `my-operator` and `broken-operator` show `Ready=False`. The difference is `Progressing`: True means active work (normal), False means stuck (needs attention). The pattern to watch for is `Ready=False, Progressing=False` — that's the red flag.

---

### 3.25 CE Progress Deadline Exceeded (Pre-COS Error)

The user's ServiceAccount lacks RBAC permissions. The CE has been retrying for 30 minutes (the default `progressDeadlineMinutes`). No COS was ever created because the error occurs before revision creation.

**Before deadline** (first 30 minutes — same as §3.12):

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS                OPERATION   TARGET   AGE
my-operator   1.0.0     True    True          AuthorizationFailed   Upgrade     2.0.0    5d
```

**After deadline fires** (30+ minutes):

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS                     OPERATION   TARGET   AGE
my-operator   1.0.0     True    False         ProgressDeadlineExceeded   Upgrade     2.0.0    5d
```

```
$ kubectl get clusterextensions -o wide
NAME          VERSION   READY   PROGRESSING   STATUS                     OPERATION   TARGET   MESSAGE                                                                          AGE
my-operator   1.0.0     True    False         ProgressDeadlineExceeded   Upgrade     2.0.0    Progress deadline exceeded after 30 minutes. Last error: pre-authorization ...   5d
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
    status: "True"
    reason: Succeeded
    message: "All managed resources are healthy"
    observedGeneration: 2
    lastTransitionTime: "2026-07-02T08:00:00Z"
  - type: Progressing
    status: "False"
    reason: ProgressDeadlineExceeded
    message: "Progress deadline exceeded after 30 minutes. Last error: pre-authorization failed: service account requires the following permissions: [create deployments.apps in namespace my-ns]"
    observedGeneration: 2
    lastTransitionTime: "2026-07-07T10:30:00Z"
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

**Key UX point**: The transition from `Progressing=True/AuthorizationFailed` to `Progressing=False/ProgressDeadlineExceeded` is the critical signal. During the first 30 minutes, `Progressing=True` means "the system is still trying." After the deadline, `Progressing=False` means "this needs attention — it won't fix itself." The last error is preserved in the message so the user knows *what* timed out, not just *that* it timed out.

`Ready=True` — the old version is still healthy and unaffected. `operation` persists so the user can see what they were trying to roll out to.

**Recovery**: If the user grants the required RBAC permissions, the next successful reconcile transitions back to `Progressing=True/Deploying` (COS creation begins) and eventually `Progressing=False/Succeeded`. The deadline is not a permanent tombstone — it is a signal, not a gate.

**User action**: Check the preserved error message, fix the RBAC permissions, and wait for the controller to retry.

---

### 3.26 CE Progress Deadline Exceeded (Resolution — No Previous Install)

A first-time install where the package name is wrong. The resolution has been failing for 30 minutes.

```
$ kubectl get clusterextensions
NAME          VERSION   READY   PROGRESSING   STATUS                     OPERATION   TARGET   AGE
my-operator   <none>    False   False         ProgressDeadlineExceeded                        35m
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
    message: "No bundle installed"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: ProgressDeadlineExceeded
    message: "Progress deadline exceeded after 30 minutes. Last error: no bundles found for package \"my-operator\" matching version \">=1.0.0\" in channels [stable]"
    observedGeneration: 1
    lastTransitionTime: "2026-07-07T10:30:00Z"
  install: null
  operation: null
```

**Key UX point**: `Ready=False, Progressing=False` — the red flag pattern. Both conditions are False with non-success reasons, unambiguously signaling "needs attention." The preserved error tells the user exactly what failed. No `operation` because resolution never succeeded (we don't know the target bundle).

Compare with §3.5 (same error, before deadline): `Progressing=True/ResolutionFailed` — the system is still trying and might succeed if a catalog update adds the bundle. After the deadline, the signal changes to "this probably won't fix itself."

**User action**: Fix the package name, version constraint, or channel. Or add a catalog containing the desired package.

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
| `ReasonDeploying` | `"Deploying"` | CE: Progressing=True, Ready=False | ✓ | — |
| `ReasonRollingOut` | `"RollingOut"` | COS: Progressing=True, Ready=False | — | ✓ |
| `ReasonResolutionFailed` | `"ResolutionFailed"` | Progressing=True | ✓ | — |
| `ReasonImagePullFailed` | `"ImagePullFailed"` | Progressing=True | ✓ | — |
| `ReasonPreflightFailed` | `"PreflightFailed"` | Progressing=True | ✓ | — |
| `ReasonAuthorizationFailed` | `"AuthorizationFailed"` | Progressing=True | ✓ | — |
| `ReasonUnsupportedContent` | `"UnsupportedContent"` | Progressing=False | ✓ | — |
| `ReasonSafetyCheckFailed` | `"SafetyCheckFailed"` | Progressing=True | ✓ | — |
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
| `ClusterObjectSetReasonArchived` | `"Archived"` | Progressing=False |
| `ClusterObjectSetReasonProbesSucceeded` | `"ProbesSucceeded"` | Ready=True |
| `ClusterObjectSetReasonReconciling` | `"Reconciling"` | Ready=Unknown |
| `ClusterObjectSetReasonObjectCollisionDetected` | `"ObjectCollisionDetected"` | Progressing=True |
| `ClusterObjectSetReasonValidationFailed` | `"ValidationFailed"` | Progressing=True |

**Removed:** `ClusterObjectSetReasonBlocked` (use shared `ReasonBlocked`), `ClusterObjectSetReasonProbeFailure` (use shared `ReasonProbeFailure`), `ClusterObjectSetReasonRetrying` (use shared `ReasonRetrying`).

**Dropped:** `"Migrated"` — documented in previous API comments but never implemented in code. With `succeededAt` replacing the Succeeded condition, migrated revisions simply get `succeededAt` set.

### 4.4 Complete Condition × Reason Matrix

**ClusterExtension:**

| Condition | True | False | Unknown |
|-----------|------|-------|---------|
| **Installed** | Succeeded | Absent | — |
| **Ready** | Succeeded | Absent, ProbeFailure, Deploying | Pending |
| **Progressing** | Deploying, ProbeFailure, ResolutionFailed, ImagePullFailed, PreflightFailed, AuthorizationFailed, SafetyCheckFailed, Retrying | Succeeded, Blocked, InvalidConfiguration, UnsupportedContent, ProgressDeadlineExceeded | — |
| **Deprecated** | Deprecated | NotDeprecated | DeprecationStatusUnknown |
| **PackageDeprecated** | Deprecated | NotDeprecated | DeprecationStatusUnknown |
| **ChannelDeprecated** | Deprecated | NotDeprecated | DeprecationStatusUnknown |
| **BundleDeprecated** | Deprecated | NotDeprecated | DeprecationStatusUnknown, Absent |

**ClusterObjectSet:**

| Condition | True | False | Unknown |
|-----------|------|-------|---------|
| **Ready** | ProbesSucceeded | ProbeFailure, RollingOut | Reconciling |
| **Progressing** | RollingOut, ObjectCollisionDetected, ValidationFailed, Retrying | Succeeded, Blocked, Archived, ProgressDeadlineExceeded | — |

### 4.5 Reason Semantics

| Reason | Meaning | Self-resolving? |
|--------|---------|-----------------|
| `Succeeded` | Operation completed successfully | N/A (terminal success) |
| `Absent` | Resource does not exist yet (neutral, not an error) | N/A |
| `Pending` | Waiting for initial state to be established | N/A |
| `Deploying` | CE: active deployment in progress, no issues | N/A (progressing) |
| `RollingOut` | COS: active phased rollout in progress | N/A (progressing) |
| `ProbeFailure` | One or more readiness probes failing (on Ready: health; on Progressing: rollout stuck on probes) | Context-dependent |
| `ResolutionFailed` | Bundle resolution failed (package/version not found, ambiguous) | Yes |
| `ImagePullFailed` | Bundle image pull failed (auth, network, missing image) | Yes |
| `AuthorizationFailed` | RBAC pre-authorization failed (ServiceAccount lacks permissions) | Yes |
| `UnsupportedContent` | Bundle content unsupported (apiServiceDefinitions, install modes) | No |
| `SafetyCheckFailed` | CRD safety check failed (CRD upgrade safety, etc.) | Yes (but may persist until bundle or config changes) |
| `ObjectCollisionDetected` | Object ownership conflict — another controller owns the resource | Yes |
| `PreflightFailed` | CE preflight check failed (ServiceAccount not found, other CE-level validation) | Yes |
| `ValidationFailed` | COS dry-run or admission validation failed (webhook rejection, schema validation) | Yes |
| `Retrying` | COS-level transient error (secret resolution, watch setup, engine error) | Yes |
| `Blocked` | Terminal error requiring manual intervention | No |
| `InvalidConfiguration` | User configuration error requiring spec change | No |
| `ProgressDeadlineExceeded` | Rollout exceeded configured time limit | No (but controller continues retrying — a successful retry recovers the condition) |
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
  - Update print columns to add Revision, Status, and Message[wide]; rename Available→Ready; drop Lifecycle (redundant with Status=Archived)
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
  - Add `ProgressDeadlineMinutes *int32` field to `ClusterExtensionSpec` with `+kubebuilder:default=30`
  - Remove `ActiveRevisions` field and `RevisionStatus` type
  - Update print columns (Version, Ready, Progressing, Status, Operation, Target, Age, Message[wide])
- `common_controller.go`:
  - Add `setReadyCondition()` helper functions
  - Update `setInstalledStatusFromRevisionStates()` to also set Ready
  - Ready derivation: reflect installed COS probe state when installed; reflect pipeline error state when not installed
- `clusterextension_controller.go`:
  - Add CE-level progress deadline tracking: record the time when `Progressing` transitions to `True` (new `observedGeneration`); on each reconcile, check elapsed time against `spec.progressDeadlineMinutes`; if exceeded, transition `Progressing` to `False/ProgressDeadlineExceeded` with the last error message preserved
  - Implement `deadlineAwareRateLimiter` for the CE controller (similar pattern to the COS controller's `progress_deadline.go`) to ensure timely reconciliation when the deadline expires during exponential backoff
  - Update `SetDeprecationStatus` (unchanged but verify no interaction)
  - Update `ensureFailureConditionsWithReason` for new condition set (add Ready)
- `boxcutter_reconcile_steps.go`:
  - Stop mirroring COS conditions; translate COS state into CE-native Ready/Progressing
  - Populate `status.operation` with type determination logic (Install/Upgrade/Reconfigure)
  - Remove `activeRevisions` population
  - Use `cos.Status.SucceededAt != nil` instead of `Succeeded=True` condition for installed classification
  - When creating a COS, set `cos.Spec.ProgressDeadlineMinutes` to the remaining time from the CE deadline (pipeline-wide budget)
- `conditionsets/conditionsets.go`:
  - Add `TypeReady` to `ConditionTypes`
  - Add `ReasonProbeFailure`, `ReasonPending`, `ReasonResolutionFailed`, `ReasonImagePullFailed`, `ReasonPreflightFailed`, `ReasonAuthorizationFailed`, `ReasonUnsupportedContent`, `ReasonSafetyCheckFailed` to `ConditionReasons`
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
| Progressing reason changed | Warning | ProgressingReasonChanged | `"Progressing reason changed from Deploying to ProbeFailure: Deployment my-ns/my-deploy not ready"` |
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

## Part 7: Recommended Alerting and Automation Patterns

This section provides operational guidance for teams integrating CE/COS status into monitoring, alerting, and CI/CD pipelines.

### 7.1 Alerting Rules

With the CE progress deadline (§1.9), a single alert pattern covers all failure modes that need human attention:

**Critical — Needs immediate attention** (extension is stuck and won't self-resolve):

```
CE Progressing == "False" AND CE Progressing.reason NOT IN ("Succeeded")
```

This fires for `Blocked`, `InvalidConfiguration`, `UnsupportedContent`, and `ProgressDeadlineExceeded`. All four require human intervention. Reason-specific routing can direct to different runbooks:
- `Blocked` → check the Progressing message for the specific blocking error (immutable secrets, content digest mismatch, malformed image)
- `InvalidConfiguration` → fix the CE spec (configuration schema error)
- `UnsupportedContent` → use a different bundle version that doesn't use unsupported features
- `ProgressDeadlineExceeded` → check the preserved last error in the message; the original failure reason tells you what to fix

**Warning — Retrying, may need attention** (optional, for teams that want earlier visibility):

```
CE Progressing == "True" AND CE Progressing.reason NOT IN ("Deploying") FOR > 10m
```

This fires for `*Failed` reasons that have been retrying for a while. It catches issues before the progress deadline fires, for teams that want proactive alerting. The duration threshold avoids noise from brief transient errors.

**Health check — Extension is unhealthy**:

```
CE Ready == "False" AND CE Progressing == "False"
```

This is the red flag pattern: nothing is healthy AND nothing is being done about it. Note: `Ready=False AND Progressing=True` is *normal* during upgrades — do NOT alert on that combination alone, or every upgrade will fire an alert.

### 7.2 kubectl wait Patterns for CI/CD

```bash
# Wait for install/upgrade to complete (rollout finished)
kubectl wait clusterextension/my-operator \
  --for=condition=Progressing=False --timeout=600s

# Wait for healthy (managed resources passing probes)
kubectl wait clusterextension/my-operator \
  --for=condition=Ready=True --timeout=600s

# Wait for both — full rollout complete AND healthy
# (run sequentially — Progressing=False first, then Ready=True)
kubectl wait clusterextension/my-operator \
  --for=condition=Progressing=False --timeout=600s
kubectl wait clusterextension/my-operator \
  --for=condition=Ready=True --timeout=60s

# Check for failure (non-zero exit if stuck)
kubectl wait clusterextension/my-operator \
  --for=jsonpath='{.status.conditions[?(@.type=="Progressing")].reason}'=Succeeded \
  --timeout=600s
```

**Important**: `kubectl wait --for=condition=Progressing=False` will succeed for BOTH `Succeeded` (good) AND `Blocked`/`ProgressDeadlineExceeded` (bad). CI/CD pipelines should check the reason after the wait returns:

```bash
kubectl wait clusterextension/my-operator \
  --for=condition=Progressing=False --timeout=600s

REASON=$(kubectl get clusterextension/my-operator \
  -o jsonpath='{.status.conditions[?(@.type=="Progressing")].reason}')
if [ "$REASON" != "Succeeded" ]; then
  echo "Rollout failed with reason: $REASON"
  exit 1
fi
```

### 7.3 Fleet Monitoring at Scale

For teams managing many extensions, quick filtering for problems:

```bash
# Show only extensions that need attention
kubectl get clusterextensions -o wide | grep -E '(False\s+False|ProgressDeadlineExceeded|Blocked|InvalidConfiguration)'

# Show all COS objects for a specific extension
kubectl get clusterobjectsets -l olm.operatorframework.io/owner-name=my-operator

# Show only actively rolling-out COS objects
kubectl get clusterobjectsets | grep RollingOut
```

### 7.4 Prometheus Metrics (Future Work)

For fleet-scale dashboards and PagerDuty integration, the conditions should be exposed as metrics. This is out of scope for this RFC, but the recommended shape is:

```
olm_clusterextension_condition{name, condition, status, reason} = 1
```

This enables Grafana queries like:
- "How many extensions are healthy?" → `count(olm_clusterextension_condition{condition="Ready", status="True"})`
- "Which extensions are stuck?" → `olm_clusterextension_condition{condition="Progressing", status="False", reason!="Succeeded"}`

The kube-state-metrics project can generate these from standard Kubernetes conditions without custom instrumentation.

# **Benefit**

1. **Self-sufficient CE**: Users can understand extension health, installation status, and rollout progress entirely from the CE, without inspecting COS resources. The `Ready` condition provides a clear health signal, and `status.operation` shows upgrade context.

2. **Correct semantics**: `Progressing=False/Succeeded` follows Kubernetes conventions. Users and tooling (like `kubectl wait --for=condition=Progressing=False`) work as expected.

3. **Phase-level debugging on COS**: When users do need to investigate a stuck rollout, COS provides per-phase status with probe failure details, eliminating the need to inspect individual managed objects.

4. **Better kubectl experience**: Print columns show what matters — `Ready`, `Progressing`, and `Status` give an at-a-glance health summary and triage signal, `Version` shows what's installed, and `Operation`/`Target` show what's happening.

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

### Alternative 4: Operation as CE Lifecycle State

Instead of `status.operation` being a transient field (nil in steady state, populated during rollouts), model it as a persistent lifecycle state that always describes what the CE controller is doing. This would expand the `OperationType` enum to include steady-state phases:

```go
// +kubebuilder:validation:Enum=Install;Upgrade;Reconfigure;Downgrade;Monitoring
Type OperationType `json:"type"`
```

| State | Meaning |
|-------|---------|
| `Install` | First-time installation in progress |
| `Upgrade` | Version upgrade in progress |
| `Downgrade` | Version downgrade in progress |
| `Reconfigure` | Same-version configuration change in progress |
| `Monitoring` | Steady state — watching managed resources for drift and self-healing |

In this model, `operation` is never nil. Drift recovery (§3.1a) naturally fits as the `Monitoring` state — the CE is monitoring resources, detected drift, and is self-healing. The transition from `Monitoring` to `Upgrade` or `Reconfigure` happens when the user changes the spec.

**Why not chosen for this RFC**:

1. **Noise in the common case**: Most of the time, the extension is in steady state. Having `OPERATION=Monitoring` in every print column row adds a column value that is almost always the same — it's information without signal. The current proposal uses empty `OPERATION` and `TARGET` columns in steady state, keeping the default `kubectl get` output clean for the common case.

2. **Mixes concerns**: The current `operation` answers "what rollout is happening?" — a transient event with a clear start and end. A lifecycle state answers "what mode is the controller in?" — a persistent classification. These are different questions. Conditions (`Ready`, `Progressing`) already capture the controller's mode; `operation` is specifically for rollout context.

3. **Downgrade adds complexity without clear need**: Distinguishing downgrades from upgrades requires the controller to compare semantic versions, which is fragile across versioning schemes. The RFC's resolved question #5 notes that `Upgrade` can cover all version changes; `Downgrade` can be added non-breakingly later if users demonstrate a need.

4. **Drift recovery is already well-expressed**: The combination of `Ready=False/ProbeFailure`, `Progressing=True/ProbeFailure`, and `operation=nil` clearly signals drift recovery in the current proposal. The absence of `operation` is itself the signal — "something is wrong but it's not a rollout." Adding a `Monitoring` state would make this more explicit but at the cost of the nil-means-nothing-interesting simplicity.

This alternative could be revisited if users find the nil `operation` during drift recovery confusing, or if the lifecycle-state model proves more natural for GitOps tooling that wants to classify CE state categorically.

# **Non-goals**

- **Service-level health checks**: OLM manages Kubernetes resources, not application health. The `Ready` condition reflects resource-level probe status, not application-level availability. Application-level health monitoring is out of scope.

- **Historical rollout tracking**: This RFC does not propose rollout history on the CE. Past rollouts can be observed through COS resources (retained up to the retention limit of 5) and Kubernetes events.

- **COS spec changes**: This RFC only modifies COS status fields. COS spec (phases, probes, collision protection) is unchanged.

- **Helm applier changes**: The status improvements focus on the boxcutter applier path. The Helm applier is being deprecated and is not covered by the COS phase status changes.

# **Key Dependencies and Open Questions**

### Resolved Questions

1. **Boxcutter library phase data**: ✅ Resolved. The boxcutter `RevisionResult` interface provides `GetPhases() []PhaseResult`, where each `PhaseResult` has `GetName()`, `IsComplete()`, `InTransition()`, `GetValidationError()`, and `GetObjects() []ObjectResult`. Each `ObjectResult` has `ProbeResults() ProbeResultContainer` with per-probe status and messages. The implementation maps these to `PhaseStatus` as described in §2.3. Phase names for "Pending" phases come from `cos.Spec.Phases[*].Name` (the full list) minus the phases in `RevisionResult.GetPhases()` (only processed phases).

2. **Rollout field lifecycle**: ✅ Resolved. `status.operation` persists whenever a rollout has been attempted, including failed/blocked rollouts. It is cleared only on `Progressing=False/Succeeded`. This ensures the user can always see what they were trying to roll out to, even if it failed.

3. **Ready condition during upgrades**: ✅ Resolved. Ready tracks the latest active COS's probe state to reflect the actual on-cluster state. During an upgrade, Ready drops to `False/Deploying` because objects are in transition between revisions. When a pre-COS error occurs and no new COS is created, Ready stays based on the installed revision (the on-cluster state hasn't changed). Ready does not carry pipeline error detail — that is Progressing's job. This follows the Kubernetes Deployment pattern where Available and Progressing are orthogonal. The pattern `Ready=False, Progressing=True` is normal during upgrades; the red flag is `Ready=False, Progressing=False` (stuck).

4. **Phase status for migrated revisions**: ✅ Resolved. Migrated COS revisions (from `BoxcutterStorageMigrator`) did not go through the phased rollout process. Their `status.phases` will be empty, and they will have `succeededAt` set. This is acceptable because migrated revisions represent pre-existing workloads that were already running.

5. **Rollout type for downgrades**: ✅ Resolved. Not adding a `Downgrade` type now. `Upgrade` covers all version changes regardless of direction. A `Downgrade` type can be added non-breakingly later (it's an additive enum change) if users or tooling demonstrate a need for the distinction.

### Open Questions

1. **Backward compatibility for condition consumers**: Tooling that watches for `Progressing=True/Succeeded` will break when the semantics change to `Progressing=False/Succeeded`. This needs to be communicated clearly in release notes. Since the COS API and the experimental CE fields are not yet GA, this is less of a concern for those changes. The CE `Progressing` fix is a bug fix and should be documented as such.

2. **Phase status message size**: Probe failure messages can be verbose (they include full GVK, namespace/name, and probe details). For phases with many objects, the concatenated message could be large. The current COS controller breaks after the first failing object per phase. We should adopt a similar strategy and potentially limit message size.

3. **Ready condition and resource drift**: Since Ready now tracks the latest active COS's probe state, drift on the installed revision would be reflected if the COS re-reconciles and temporarily reports probes failing. This is consistent with the "track actual on-cluster state" principle — if probes are temporarily failing, Ready should reflect that. The condition will recover once the COS re-reconciles and probes pass again.

# **RACI**

| R​esponsible | TBD |
| :---- | :---- |
| **A**​ccountable | TBD |
| **C**​onsulted | OLM v1 team, UX team |
| **I**​nformed | OLM community |
