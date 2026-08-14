package ingestion

type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

func (s State) Terminal() bool { return s == StateCompleted || s == StateFailed || s == StateCanceled }
func CanTransition(from, to State) bool {
	switch from {
	case StatePending:
		return to == StateRunning || to == StateCanceled
	case StateRunning:
		return to == StateCompleted || to == StateFailed || to == StateCanceled
	}
	return false
}
