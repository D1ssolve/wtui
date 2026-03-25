package modal

type SubmitInitMsg struct {
	TaskID       string
	Services     []string // parsed from comma or space separated input
	BranchPrefix string
	BaseBranch   string
}

type SubmitAddMsg struct {
	TaskID   string
	Services []string // parsed from comma or space separated input
}

type SubmitRemoveTaskMsg struct {
	TaskID         string
	Force          bool
	DeleteBranches bool
}

type CloseModalMsg struct{}

type SubmitCloneMsg struct {
	Src string
	Dst string
}

type SubmitRemoveServiceMsg struct {
	TaskID       string
	ServiceName  string
	RemoveBranch bool
}
