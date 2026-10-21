package status

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pkg.package-operator.run/boxcutter/machinery"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// sumObjectCounts aggregates ObjectCounts across all observed phases into a single total.
// Returns ObjectCounts with a Total of zero if phases is empty.
func sumObjectCounts(phases []ocv1.ObservedPhase) ocv1.ObjectCounts {
	counts := ocv1.ObjectCounts{
		Total: 0,
	}
	if len(phases) == 0 {
		return counts
	}
	for i := range phases {
		counts.Total += phases[i].ObjectCounts.Total
		counts.Present += phases[i].ObjectCounts.Present
		counts.Synced += phases[i].ObjectCounts.Synced
		counts.Ready += phases[i].ObjectCounts.Ready
	}
	return counts
}

func UpdateObservedPhasesForReconcile(objSet *ocv1.ClusterObjectSet, rres machinery.RevisionResult) {
	if objSet == nil || rres.GetValidationError() != nil {
		return
	}

	// Calculate totals from the spec
	phaseTotals := make(map[string]int64)
	for _, phase := range objSet.Spec.Phases {
		phaseTotals[phase.Name] = int64(len(phase.Objects))
	}

	// Phases are reconciled in order; once a phase is incomplete, later phases are
	// only observed read-only (paused). A paused phase's preflight dry-run can fail
	// for reasons that resolve once earlier phases roll out (e.g. a Namespace a
	// later phase depends on has not been created yet), so its validation error is
	// not actionable yet. Only treat a phase's validation error as a terminal
	// validation failure while every preceding phase is complete; otherwise skip it
	// and let the rollout status (and the progress deadline) below take over.
	// Collisions are still reported for paused phases below: an object owned by
	// another controller does not become ownable just because earlier phases roll
	// out, so it remains actionable regardless of phase ordering.
	precedingPhasesComplete := true
	for _, pres := range rres.GetPhases() {
		observedPhase := ocv1.ObservedPhase{
			Name: pres.GetName(),
		}
		if pres.GetValidationError() != nil {
			if !precedingPhasesComplete {
				observedPhase.Status = ocv1.PhaseStatusPending
				observedPhase.Message = "waiting for earlier phases to complete"
			} else {
				observedPhase.Status = ocv1.PhaseStatusInvalid
				observedPhase.Message = truncateMessage(pres.GetValidationError().Error())
			}
			updateObservedPhaseStatus(objSet, observedPhase)
			continue
		}

		if !pres.IsComplete() {
			precedingPhasesComplete = false
		}

		isPaused := false
		observedPhase.ObjectCounts.Total = phaseTotals[pres.GetName()]
		for _, pobj := range pres.GetObjects() {
			if pobj.IsComplete() {
				observedPhase.ObjectCounts.Ready++
			}
			switch pobj.Action() {
			case machinery.ActionCreated:
				if !pobj.IsPaused() {
					observedPhase.ObjectCounts.Present++
					observedPhase.ObjectCounts.Synced++
				}
			case machinery.ActionIdle:
				observedPhase.ObjectCounts.Present++
				observedPhase.ObjectCounts.Synced++
			case machinery.ActionProgressed:
				observedPhase.ObjectCounts.Present++
			case machinery.ActionUpdated, machinery.ActionRecovered:
				observedPhase.ObjectCounts.Present++
				if !pobj.IsPaused() {
					observedPhase.ObjectCounts.Synced++
				}
			case machinery.ActionCollision:
				// TODO: collect object collisions and put in status
			}

			// track for phase status derivation
			if pobj.IsPaused() {
				isPaused = true
			}
		}

		var (
			// TODO: consider reframing as one or more objects have transitioned (rather than all - since there can be differences b/w the revisions)
			isSuperseded  = pres.HasProgressed()
			isReady = pres.IsComplete() && !isSuperseded
			isSynced      = observedPhase.ObjectCounts.Synced == observedPhase.ObjectCounts.Total
			isReconciling = !isSynced && !isPaused
		)

		switch {
		case isReady:
			observedPhase.Status = ocv1.PhaseStatusAvailable
			observedPhase.Message = "all objects are ready"
		case isSuperseded:
			observedPhase.Status = ocv1.PhaseStatusSuperseded
			observedPhase.Message = "all objects have progressed to a new revision"
		case isSynced:
			// TODO: collect assertion failures and add to status
			observedPhase.Status = ocv1.PhaseStatusWaitingForAssertions
			observedPhase.Message = "phase is waiting some objects to reach the desired state"
		case isReconciling:
			// TODO: deal with surfacing collisions
			// TODO: reconsider splitting into the cases where reconciling might appear (e.g. ObjectCollision, objects are transitioning to another revision, etc.)
			observedPhase.Status = ocv1.PhaseStatusReconciling
			observedPhase.Message = "phase is being reconciled"
		default:
			// read-only while an earlier phase is still rolling out.
			observedPhase.Status = ocv1.PhaseStatusPending
			observedPhase.Message = "waiting for previous phases to complete"
		}
		updateObservedPhaseStatus(objSet, observedPhase)
	}
	objSet.Status.ObjectCounts = sumObjectCounts(objSet.Status.ObservedPhases)
}

func UpdateObservedPhasesForTeardown(objSet *ocv1.ClusterObjectSet, tres machinery.RevisionTeardownResult) {
	if objSet == nil {
		return
	}

	// Calculate totals from the spec
	phaseTotals := make(map[string]int64)
	for _, phase := range objSet.Spec.Phases {
		phaseTotals[phase.Name] = int64(len(phase.Objects))
	}

	for _, pres := range tres.GetPhases() {
		observedPhase := ocv1.ObservedPhase{
			Name: pres.GetName(),
		}

		observedPhase.ObjectCounts.Total = phaseTotals[pres.GetName()]
		observedPhase.ObjectCounts.Present = int64(len(pres.Waiting()))
		observedPhase.ObjectCounts.Synced = 0

		if name, ok := tres.GetActivePhaseName(); ok && name == pres.GetName() {
			observedPhase.Status = ocv1.PhaseStatusTearingDown
			observedPhase.Message = "removing all phase objects owned by this object set"
		} else if slices.Contains(tres.GetGonePhaseNames(), pres.GetName()) {
			observedPhase.Status = ocv1.PhaseStatusTeardownComplete
			observedPhase.Message = "all phase object owned this object set have been removed"
		} else {
			observedPhase.Status = ocv1.PhaseStatusPending
			observedPhase.Message = "waiting for previous phases to complete"
		}
		updateObservedPhaseStatus(objSet, observedPhase)
	}
	objSet.Status.ObjectCounts = sumObjectCounts(objSet.Status.ObservedPhases)
}

func updateObservedPhaseStatus(objSet *ocv1.ClusterObjectSet, observedPhase ocv1.ObservedPhase) {
	if objSet == nil {
		return
	}
	now := metav1.Now()
	for i, ophase := range objSet.Status.ObservedPhases {
		if ophase.Name != observedPhase.Name {
			continue
		}
		// Digest is computed once at resolution time (before controller mutations)
		// and is immutable; carry it forward so the full-struct replacement below
		// does not drop the required field.
		observedPhase.Digest = ophase.Digest
		// Preserve CompletedAt once set.
		if ophase.CompletedAt != nil {
			observedPhase.CompletedAt = ophase.CompletedAt
		}
		if observedPhase.Status == ocv1.PhaseStatusAvailable && observedPhase.CompletedAt == nil {
			observedPhase.CompletedAt = &now
		}
		// LastTransitionTime advances only when Status changes.
		if ophase.Status != observedPhase.Status {
			observedPhase.LastTransitionTime = now
		} else {
			observedPhase.LastTransitionTime = ophase.LastTransitionTime
		}
		objSet.Status.ObservedPhases[i] = observedPhase
		return
	}
	// First time this phase appears in the status.
	if observedPhase.Status == ocv1.PhaseStatusAvailable && observedPhase.CompletedAt == nil {
		observedPhase.CompletedAt = &now
	}
	observedPhase.LastTransitionTime = now
	objSet.Status.ObservedPhases = append(objSet.Status.ObservedPhases, observedPhase)
}

// FromReconcile updates the status of the given ClusterObjectSet based on a reconcile result,
// populating the phase level and aggregate ObjectCounts.
func FromReconcile(cos *ocv1.ClusterObjectSet, result machinery.RevisionResult, now metav1.Time) {
	if cos == nil {
		return
	}
	observedPhasesFromReconcileResult(cos.Spec.Phases, &cos.Status.ObservedPhases, result)
	preserveCompletionTimes(&cos.Status.ObservedPhases, now)
	cos.Status.ObjectCounts = sumObjectCounts(cos.Status.ObservedPhases)
}

// observedPhasesFromReconcileResult populates observedPhases from a reconcile result.
// When the revision has progressed (i.e. is transitioning), only Total counts are preserved from existing
// observed phases; otherwise full per-object counts are derived from phase results.
func observedPhasesFromReconcileResult(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, result machinery.RevisionResult) {
	if observedPhases == nil || result == nil {
		return
	}
	if result.HasProgressed() {
		allPhasesWithStatus(specPhases, observedPhases, ocv1.PhaseStatusSuperseded)
	} else {
		buildObservedPhases(specPhases, observedPhases, result.GetPhases())
	}
}

// allPhasesWithStatus sets every observed phase to status and updates each phase's
// object count total from specPhases. Used when a revision is progressing and
// detailed per-object results are not yet available.
func allPhasesWithStatus(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, status ocv1.PhaseStatus) {
	objTotalCountMap := make(map[string]int)
	for _, specPhase := range specPhases {
		objTotalCountMap[specPhase.Name] = len(specPhase.Objects)
	}
	op := *observedPhases
	for i := range op {
		op[i].ObjectCounts.Total = int64(objTotalCountMap[op[i].Name])
		op[i].Status = status
		// Clear any potentially outdated messages
		op[i].Message = ""
	}
	*observedPhases = op
}

// buildObservedPhases constructs observed phases from reconcile phase results.
// Complete phases receive full counts (Total=Present=Synced=Ready); incomplete phases are handled by incompletePhase.
func buildObservedPhases(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, phaseResults []machinery.PhaseResult) {
	mapSpecPhases(specPhases, observedPhases, phaseResults, ocv1.PhaseStatusAvailable, "Phase was not evaluated",
		func(total int64) ocv1.ObjectCounts {
			return ocv1.ObjectCounts{Total: total, Present: total, Synced: total, Ready: total}
		},
		incompletePhase,
	)
}

// incompletePhase computes ObjectCounts for a phase whose reconcile result is not yet complete.
// Present counts all objects that exist on the cluster (non-paused, or paused but not freshly created).
// Synced counts objects that are idle (no pending changes). Ready counts objects reported as complete.
func incompletePhase(sp ocv1.ClusterObjectSetPhase, pr machinery.PhaseResult, observedPhase *ocv1.ObservedPhase) {
	observedPhase.ObjectCounts = ocv1.ObjectCounts{Total: int64(len(sp.Objects))}

	paused := false
	for _, obj := range pr.GetObjects() {
		if !obj.IsPaused() || obj.Action() != machinery.ActionCreated {
			observedPhase.ObjectCounts.Present++
		}
		if obj.IsPaused() {
			paused = true
			if obj.Action() == machinery.ActionIdle {
				observedPhase.ObjectCounts.Synced++
			}
		} else {
			switch obj.Action() {
			case machinery.ActionIdle, machinery.ActionUpdated, machinery.ActionCreated, machinery.ActionRecovered:
				observedPhase.ObjectCounts.Synced++
			}
		}
		if obj.IsComplete() {
			observedPhase.ObjectCounts.Ready++
		}
	}

	switch {
	case paused:
		observedPhase.Status = ocv1.PhaseStatusPending
		observedPhase.Message = "Waiting for earlier phases to complete"
	case observedPhase.ObjectCounts.Synced == observedPhase.ObjectCounts.Total:
		observedPhase.Status = ocv1.PhaseStatusWaitingForAssertions
	default:
		observedPhase.Status = ocv1.PhaseStatusReconciling
	}
}

// FromTeardown updates the status of the given ClusterObjectSet based on a teardown result,
// populating ObservedPhases and the aggregate ObjectCounts.
func FromTeardown(cos *ocv1.ClusterObjectSet, result machinery.RevisionTeardownResult, now metav1.Time) {
	if cos == nil {
		return
	}
	observedPhasesFromTeardownResult(cos.Spec.Phases, &cos.Status.ObservedPhases, result)
	preserveCompletionTimes(&cos.Status.ObservedPhases, now)
	cos.Status.ObjectCounts = sumObjectCounts(cos.Status.ObservedPhases)
}

// observedPhasesFromTeardownResult populates observedPhases from a teardown result.
func observedPhasesFromTeardownResult(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, result machinery.RevisionTeardownResult) {
	if result == nil || observedPhases == nil {
		return
	}
	activeName, _ := result.GetActivePhaseName()
	buildTeardownObservedPhases(specPhases, observedPhases, result.GetPhases(), activeName)
}

// buildTeardownObservedPhases constructs observed phases from teardown phase results.
// Complete phases receive only the Total count; incomplete phases are handled by tearingDownPhase.
func buildTeardownObservedPhases(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, phaseResults []machinery.PhaseTeardownResult, activePhaseName string) {
	mapSpecPhases(specPhases, observedPhases, phaseResults, ocv1.PhaseStatusTeardownComplete, "",
		func(total int64) ocv1.ObjectCounts {
			return ocv1.ObjectCounts{Total: total}
		},
		func(sp ocv1.ClusterObjectSetPhase, pr machinery.PhaseTeardownResult, op *ocv1.ObservedPhase) {
			tearingDownPhase(sp, pr, pr.GetName() == activePhaseName, op)
		},
	)
}

// tearingDownPhase sets ObjectCounts for a phase that is still being torn down.
// Total is derived from the spec; Present reflects objects still waiting to be deleted.
func tearingDownPhase(sp ocv1.ClusterObjectSetPhase, pr machinery.PhaseTeardownResult, active bool, op *ocv1.ObservedPhase) {
	if op == nil {
		return
	}
	op.ObjectCounts = ocv1.ObjectCounts{
		Total:   int64(len(sp.Objects)),
		Present: int64(len(pr.Waiting())),
	}

	if active {
		op.Status = ocv1.PhaseStatusTearingDown
	} else {
		op.Status = ocv1.PhaseStatusPending
		op.Message = "Waiting for later phases to complete teardown"
	}
}

// mapSpecPhases merges spec phase definitions with evaluated phase results into observedPhases.
// For each spec phase: if no result exists, only Total is set; if the result is complete,
// completeObjectCounts is called; otherwise buildIncomplete fills in partial counts.
// The output slice is ordered to match specPhases.
func mapSpecPhases[T interface {
	GetName() string
	IsComplete() bool
}](
	specPhases []ocv1.ClusterObjectSetPhase,
	observedPhases *[]ocv1.ObservedPhase,
	results []T,
	completeStatus ocv1.PhaseStatus,
	unevaluatedMessage string,
	completeObjectCounts func(total int64) ocv1.ObjectCounts,
	buildIncomplete func(ocv1.ClusterObjectSetPhase, T, *ocv1.ObservedPhase),
) {
	if observedPhases == nil {
		return
	}
	resultsByName := make(map[string]T, len(results))
	for _, r := range results {
		resultsByName[r.GetName()] = r
	}

	observedPhasesMap := make(map[string]*ocv1.ObservedPhase)
	for i := range *observedPhases {
		observedPhasesMap[(*observedPhases)[i].Name] = &(*observedPhases)[i]
	}

	for _, sp := range specPhases {
		total := int64(len(sp.Objects))
		r, evaluated := resultsByName[sp.Name]
		// Digest is not set here, despite it being a required field, as by this
		// point the objects have already been mutated by the controller and
		// their digests can no longer be computed.
		if _, initialized := observedPhasesMap[sp.Name]; !initialized {
			observedPhasesMap[sp.Name] = &ocv1.ObservedPhase{Name: sp.Name}
		}
		// Clear any potentially outdated messages
		observedPhasesMap[sp.Name].Message = ""
		if !evaluated {
			observedPhasesMap[sp.Name].Status = ocv1.PhaseStatusUnknown
			observedPhasesMap[sp.Name].Message = truncateMessage(unevaluatedMessage)
			observedPhasesMap[sp.Name].ObjectCounts = ocv1.ObjectCounts{Total: total}
			continue
		}
		if r.IsComplete() {
			observedPhasesMap[sp.Name].Status = completeStatus
			observedPhasesMap[sp.Name].ObjectCounts = completeObjectCounts(total)
			continue
		}
		buildIncomplete(sp, r, observedPhasesMap[sp.Name])
	}

	result := make([]ocv1.ObservedPhase, 0, len(specPhases))
	for _, sp := range specPhases {
		if op, ok := observedPhasesMap[sp.Name]; ok {
			result = append(result, *op)
		}
	}
	*observedPhases = result
}

// preserveCompletionTimes sets CompletedAt to now on any Available phase that
// does not already have a completion time recorded.
func preserveCompletionTimes(observedPhases *[]ocv1.ObservedPhase, now metav1.Time) {
	if observedPhases == nil {
		return
	}
	for i := range *observedPhases {
		op := &(*observedPhases)[i]
		if op.CompletedAt == nil && op.Status == ocv1.PhaseStatusAvailable {
			op.CompletedAt = &now
		}
	}
}

const maxMessageLength = 1024

// truncateMessage truncates s to at most maxMessageLength runes to fit within the CRD limit.
func truncateMessage(s string) string {
	r := []rune(s)
	if len(r) <= maxMessageLength {
		return s
	}
	return string(r[:maxMessageLength])
}
