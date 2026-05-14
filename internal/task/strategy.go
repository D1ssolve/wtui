package task

type RemoteBranchStrategy int

const (
	StrategyFetchAndSwitch RemoteBranchStrategy = iota

	StrategyNewBranch

	StrategyCancel
)

func (s RemoteBranchStrategy) String() string {
	switch s {
	case StrategyFetchAndSwitch:
		return "Track Remote Branch"
	case StrategyNewBranch:
		return "New Branch"
	case StrategyCancel:
		return "Cancel"
	default:
		return "Unknown"
	}
}
