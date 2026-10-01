package domain

import "time"

type ReleaseStatus string

const (
	ReleaseStatusDraft               ReleaseStatus = "draft"
	ReleaseStatusValidating          ReleaseStatus = "validating"
	ReleaseStatusMerging             ReleaseStatus = "merging"
	ReleaseStatusBranching           ReleaseStatus = "branching"
	ReleaseStatusPushing             ReleaseStatus = "pushing"
	ReleaseStatusAwaitingTaskMerge   ReleaseStatus = "awaiting_task_merge"
	ReleaseStatusIntegratingTasks    ReleaseStatus = "integrating_tasks"
	ReleaseStatusTaskMergeBlocked    ReleaseStatus = "task_merge_blocked"
	ReleaseStatusTaskMergePartial    ReleaseStatus = "task_merge_partial"
	ReleaseStatusPrepared            ReleaseStatus = "prepared"
	ReleaseStatusAwaitingMasterMerge ReleaseStatus = "awaiting_master_merge"
	ReleaseStatusMasterMerged        ReleaseStatus = "master_merged"
	ReleaseStatusSyncingDevelop      ReleaseStatus = "syncing_develop"
	ReleaseStatusTagging             ReleaseStatus = "tagging"
	ReleaseStatusReleased            ReleaseStatus = "released"
	ReleaseStatusFailed              ReleaseStatus = "failed"
	ReleaseStatusRejected            ReleaseStatus = "rejected"
)

type Release struct {
	ManifestVersion int              `json:"manifest_version"`
	ID              string           `json:"id"`
	Title           string           `json:"title,omitempty"`
	Dir             string           `json:"dir"`
	Status          ReleaseStatus    `json:"status"`
	Checkpoint      string           `json:"checkpoint,omitempty"`
	Version         string           `json:"version,omitempty"`
	Tag             string           `json:"tag,omitempty"`
	TaskIDs         []string         `json:"task_ids"`
	Tasks           []ReleaseTaskRef `json:"tasks,omitempty"`
	Services        []ReleaseService `json:"services,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	PreparedAt      *time.Time       `json:"prepared_at,omitempty"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
	CreatedBy       string           `json:"created_by,omitempty"`
	Error           *ReleaseError    `json:"error,omitempty"`
}

type ReleaseTaskRef struct {
	TaskID       string   `json:"task_id"`
	TaskDir      string   `json:"task_dir,omitempty"`
	Phase        string   `json:"phase,omitempty"`
	ServiceNames []string `json:"service_names,omitempty"`
}

type ReleaseService struct {
	Name                    string                 `json:"name"`
	RepoPath                string                 `json:"repo_path"`
	ReleaseWorktreePath     string                 `json:"release_worktree_path,omitempty"`
	IntegrationWorktreePath string                 `json:"integration_worktree_path,omitempty"`
	IntegrationBranch       string                 `json:"integration_branch"`
	ReleaseBranch           string                 `json:"release_branch"`
	Version                 string                 `json:"version"`
	Tag                     string                 `json:"tag"`
	TagDescription          string                 `json:"tag_description,omitempty"`
	FeatureBranches         []ReleaseFeatureBranch `json:"feature_branches,omitempty"`
	Status                  ReleaseStatus          `json:"status"`
	PreIntegrationRef       string                 `json:"pre_integration_ref,omitempty"`
	PostIntegrationRef      string                 `json:"post_integration_ref,omitempty"`
	PostIntegrationSHA      string                 `json:"post_integration_sha,omitempty"`
	ReleaseRef              string                 `json:"release_ref,omitempty"`
	ReleaseSHA              string                 `json:"release_sha,omitempty"`
	ProductionMR            *ProductionMRRef       `json:"production_mr,omitempty"`
	AcceptedMergeSHA        string                 `json:"accepted_merge_sha,omitempty"`
	TagRef                  string                 `json:"tag_ref,omitempty"`
	TagSHA                  string                 `json:"tag_sha,omitempty"`
	PushedIntegration       bool                   `json:"pushed_integration"`
	PushedReleaseBranch     bool                   `json:"pushed_release_branch"`
	PushedTag               bool                   `json:"pushed_tag"`
	Error                   *ReleaseError          `json:"error,omitempty"`
}

type ProductionMRRef struct {
	Number    int    `json:"number"`
	URL       string `json:"url"`
	SourceSHA string `json:"source_sha"`
	State     string `json:"state"`
	// Repo is the canonical forge repository path (host/owner/repo) the MR
	// proof was created against; ProviderHost is the remote host when known.
	// Consumers must require both to match the service's current origin, and
	// fail closed when Repo is empty: an MR number/SHA pair from another
	// repository must never authorize a production merge.
	Repo         string `json:"repo,omitempty"`
	ProviderHost string `json:"provider_host,omitempty"`
}

type ReleaseFeatureBranch struct {
	TaskID                  string `json:"task_id"`
	ServiceName             string `json:"service_name"`
	Branch                  string `json:"branch"`
	WorktreePath            string `json:"worktree_path,omitempty"`
	Merged                  bool   `json:"merged"`
	MergeRef                string `json:"merge_ref,omitempty"`
	TaskMergeStatus         string `json:"task_merge_status,omitempty"`
	TaskMergeMRNumber       int    `json:"task_merge_mr_number,omitempty"`
	TaskMergeMRURL          string `json:"task_merge_mr_url,omitempty"`
	TaskMergeHeadSHA        string `json:"task_merge_head_sha,omitempty"`
	TaskMergeTargetSHA      string `json:"task_merge_target_sha,omitempty"`
	TaskMergeExpectedTarget string `json:"task_merge_expected_target,omitempty"`
}

type ReleaseError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Stage       string `json:"stage"`
	TaskID      string `json:"task_id,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Tag         string `json:"tag,omitempty"`
	Recoverable bool   `json:"recoverable"`
	Cause       string `json:"cause,omitempty"`
}
