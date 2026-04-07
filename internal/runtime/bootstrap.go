package runtime

// BootstrapPhase represents a phase in the application startup sequence.
type BootstrapPhase int

const (
	PhaseCliEntry BootstrapPhase = iota
	PhaseFastPathVersion
	PhaseStartupProfiler
	PhaseSystemPromptFastPath
	PhaseChromeMcpFastPath
	PhaseDaemonWorkerFastPath
	PhaseBridgeFastPath
	PhaseDaemonFastPath
	PhaseBackgroundSessionFastPath
	PhaseTemplateFastPath
	PhaseEnvironmentRunnerFastPath
	PhaseMainRuntime
)

// BootstrapPlan holds an ordered, deduplicated list of startup phases.
type BootstrapPlan struct {
	phases []BootstrapPhase
}

// DefaultBootstrapPlan returns the default claw startup sequence.
func DefaultBootstrapPlan() BootstrapPlan {
	return BootstrapPlanFromPhases([]BootstrapPhase{
		PhaseCliEntry,
		PhaseFastPathVersion,
		PhaseStartupProfiler,
		PhaseSystemPromptFastPath,
		PhaseChromeMcpFastPath,
		PhaseDaemonWorkerFastPath,
		PhaseBridgeFastPath,
		PhaseDaemonFastPath,
		PhaseBackgroundSessionFastPath,
		PhaseTemplateFastPath,
		PhaseEnvironmentRunnerFastPath,
		PhaseMainRuntime,
	})
}

// BootstrapPlanFromPhases creates a plan from the given phases, deduplicating
// while preserving order.
func BootstrapPlanFromPhases(phases []BootstrapPhase) BootstrapPlan {
	seen := make(map[BootstrapPhase]bool, len(phases))
	deduped := make([]BootstrapPhase, 0, len(phases))
	for _, p := range phases {
		if !seen[p] {
			seen[p] = true
			deduped = append(deduped, p)
		}
	}
	return BootstrapPlan{phases: deduped}
}

// Phases returns the ordered list of phases.
func (bp BootstrapPlan) Phases() []BootstrapPhase {
	return bp.phases
}
