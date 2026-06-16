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

type PromoteToReleaseMsg struct {
	TaskID   string
	Versions map[string]string
}
