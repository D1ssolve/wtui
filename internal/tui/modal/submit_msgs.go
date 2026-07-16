package modal

type SubmitPruneMsg struct {
	SelectedTaskIDs []string
}

type ForgeCreateMRMsg struct {
	TaskID      string
	ServiceName string
}

type ForgePipelineStatusMsg struct {
	TaskID      string
	ServiceName string
}

type ForgeListIssuesMsg struct {
	TaskID      string
	ServiceName string
}

type SubmitCreateReleaseMsg struct {
	TaskIDs  []string
	Versions map[string]string
}

type RequestReleaseVersionsMsg struct {
	TaskIDs []string
}

type ConfirmReleaseExecuteMsg struct {
	TaskIDs  []string
	Versions map[string]string
}

type ConfirmFinishReleaseMsg struct {
	ReleaseID string
}
