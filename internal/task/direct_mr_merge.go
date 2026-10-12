package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/forge"
)

const directMergeRoot = ".direct-merges"

type directMergeProof struct {
	Repo      string `json:"repo"`
	Number    int    `json:"number"`
	HeadSHA   string `json:"head_sha"`
	Target    string `json:"target"`
	Method    string `json:"method"`
	PushURL   string `json:"push_url"`
	TargetSHA string `json:"target_sha"`
	MergeSHA  string `json:"merge_sha"`
}

func directMergeIdentity(params forge.MergeMRParams) directMergeProof {
	method := params.Method
	if method == "" {
		method = "merge"
	}
	return directMergeProof{Repo: params.Repo, Number: params.Number, HeadSHA: params.ExpectedHeadSHA, Target: params.ExpectedTargetBranch, Method: method}
}

func (m *manager) readDirectMergeProofs(params forge.MergeMRParams) ([]directMergeProof, string, error) {
	identity := directMergeIdentity(params)
	dir := filepath.Join(m.cfg.TasksRoot, directMergeRoot, digest(identity))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, dir, nil
	}
	if err != nil {
		return nil, dir, fmt.Errorf("read direct merge proofs: %w", err)
	}
	var proofs []directMergeProof
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, dir, err
		}
		var proof directMergeProof
		if err := json.Unmarshal(data, &proof); err != nil {
			return nil, dir, fmt.Errorf("decode direct merge proof: %w", err)
		}
		gotIdentity := proof
		gotIdentity.PushURL, gotIdentity.TargetSHA, gotIdentity.MergeSHA = "", "", ""
		if gotIdentity != identity || proof.PushURL == "" || proof.TargetSHA == "" || proof.MergeSHA == "" {
			return nil, dir, errors.New("direct merge proof identity is invalid")
		}
		proofs = append(proofs, proof)
	}
	return proofs, dir, nil
}

// publishDirectMergeProof selects one immutable candidate per target SHA,
// even when two wtui processes race. Readers only see complete records.
func publishDirectMergeProof(dir string, proof directMergeProof) (directMergeProof, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return proof, err
	}
	tmp, err := os.CreateTemp(dir, ".candidate-*")
	if err != nil {
		return proof, err
	}
	defer os.Remove(tmp.Name())
	writeErr := json.NewEncoder(tmp).Encode(proof)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return proof, err
	}
	path := filepath.Join(dir, digest(proof.TargetSHA)+".json")
	if err := os.Link(tmp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return proof, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return proof, err
	}
	var selected directMergeProof
	if err := json.Unmarshal(data, &selected); err != nil {
		return proof, err
	}
	if selected.Repo != proof.Repo || selected.Number != proof.Number || selected.HeadSHA != proof.HeadSHA || selected.Target != proof.Target || selected.Method != proof.Method || selected.PushURL != proof.PushURL || selected.TargetSHA != proof.TargetSHA || selected.MergeSHA == "" {
		return proof, errors.New("direct merge candidate identity changed")
	}
	return selected, nil
}

func (m *manager) directMergeContained(ctx context.Context, repoPath string, proof directMergeProof) (bool, error) {
	tip, err := m.git.RemoteRefSHAAt(ctx, repoPath, proof.PushURL, "refs/heads/"+proof.Target)
	if err != nil {
		return false, fmt.Errorf("verify direct merge target: %w", err)
	}
	if tip == "" {
		return false, nil
	}
	if err := m.git.EnsureCommit(ctx, repoPath, tip); err != nil {
		return false, err
	}
	if err := m.git.EnsureCommit(ctx, repoPath, proof.MergeSHA); err != nil {
		return false, err
	}
	return m.git.IsAncestor(ctx, repoPath, proof.MergeSHA, tip)
}

// mergeMRWithLease never lets mutable MR metadata choose the push destination.
func (m *manager) mergeMRWithLease(ctx context.Context, repoPath string, params forge.MergeMRParams, client forge.ForgeClient) (forge.MRMergeResult, error) {
	if params.ExpectedHeadSHA == "" || params.ExpectedTargetBranch == "" || params.ExpectedTargetSHA == "" || params.Repo == "" || params.Number <= 0 {
		return forge.MRMergeResult{}, errors.New("direct merge requires complete source, target and MR identity")
	}
	pushURL, err := m.verifiedRepoPushURL(ctx, params.Repo, repoPath)
	if err != nil {
		return forge.MRMergeResult{}, err
	}
	if forge.ExtractRepoPath(pushURL) != params.Repo {
		return forge.MRMergeResult{}, errors.New("direct merge push repository does not match MR repository")
	}
	proofs, dir, err := m.readDirectMergeProofs(params)
	if err != nil {
		return forge.MRMergeResult{}, err
	}
	var proof directMergeProof
	for _, candidate := range proofs {
		if !sameRemoteURL(candidate.PushURL, pushURL) {
			return forge.MRMergeResult{}, errors.New("direct merge proof push destination changed")
		}
		contained, err := m.directMergeContained(ctx, repoPath, candidate)
		if err != nil {
			return forge.MRMergeResult{}, err
		}
		if contained {
			return forge.MRMergeResult{Merged: true, MergeCommitSHA: candidate.MergeSHA}, nil
		}
		if candidate.TargetSHA == params.ExpectedTargetSHA {
			proof = candidate
		}
	}
	fresh, err := client.MRReadinessByNumber(ctx, params.Number, params.Repo, params.WorktreePath)
	if err != nil {
		return forge.MRMergeResult{}, fmt.Errorf("direct merge MR preflight: %w", err)
	}
	if fresh.Number != params.Number || fresh.TargetBranch != params.ExpectedTargetBranch || fresh.HeadSHA != params.ExpectedHeadSHA || !openMRState(fresh.State) || !fresh.Ready || len(fresh.Blockers) != 0 {
		return forge.MRMergeResult{}, errors.New("direct merge MR changed or is not ready")
	}
	if proof.MergeSHA == "" || proof.TargetSHA != params.ExpectedTargetSHA {
		if err := m.git.EnsureCommit(ctx, repoPath, params.ExpectedHeadSHA); err != nil {
			return forge.MRMergeResult{}, err
		}
		mergeSHA, err := m.git.BuildMergeCommit(ctx, repoPath, params.ExpectedTargetSHA, params.ExpectedHeadSHA, params.Method)
		if err != nil {
			return forge.MRMergeResult{}, fmt.Errorf("build direct merge: %w", err)
		}
		descends, err := m.git.IsAncestor(ctx, repoPath, params.ExpectedTargetSHA, mergeSHA)
		if err != nil {
			return forge.MRMergeResult{}, err
		}
		if !descends {
			return forge.MRMergeResult{}, errors.New("direct merge would discard target history")
		}
		proof = directMergeIdentity(params)
		proof.PushURL, proof.TargetSHA, proof.MergeSHA = pushURL, params.ExpectedTargetSHA, mergeSHA
		// Record before push so a lost response or process exit cannot duplicate a squash/rebase.
		proof, err = publishDirectMergeProof(dir, proof)
		if err != nil {
			return forge.MRMergeResult{}, fmt.Errorf("record direct merge candidate: %w", err)
		}
	}
	ref := "refs/heads/" + params.ExpectedTargetBranch
	pushErr := m.git.PushRefWithLease(ctx, repoPath, pushURL, ref, proof.MergeSHA, ref, params.ExpectedTargetSHA)
	contained, verifyErr := m.directMergeContained(ctx, repoPath, proof)
	if verifyErr != nil {
		return forge.MRMergeResult{}, errors.Join(pushErr, verifyErr)
	}
	if !contained {
		return forge.MRMergeResult{}, errors.Join(pushErr, errors.New("direct merge result is not contained in the remote target"))
	}
	return forge.MRMergeResult{Merged: true, MergeCommitSHA: proof.MergeSHA}, nil
}

// reconcileDirectMR uses git evidence; squash/rebase need not close the forge MR.
func (m *manager) reconcileDirectMR(ctx context.Context, repoPath, method string, mr *forge.MRReadiness) (bool, error) {
	if mr.SupportsTargetBinding || mr.Number == 0 || mr.HeadSHA == "" || mr.TargetBranch == "" {
		return false, nil
	}
	remoteURL, err := m.git.RemoteURL(ctx, repoPath, "origin")
	if err != nil {
		return false, err
	}
	params := forge.MergeMRParams{Repo: forge.ExtractRepoPath(remoteURL), Number: mr.Number, ExpectedHeadSHA: mr.HeadSHA, ExpectedTargetBranch: mr.TargetBranch, Method: method}
	proofs, _, err := m.readDirectMergeProofs(params)
	if err != nil || len(proofs) == 0 {
		return false, err
	}
	pushURL, err := m.verifiedRepoPushURL(ctx, "direct merge", repoPath)
	if err != nil {
		return false, err
	}
	for _, proof := range proofs {
		if !sameRemoteURL(proof.PushURL, pushURL) {
			return false, errors.New("direct merge proof push destination changed")
		}
		contained, err := m.directMergeContained(ctx, repoPath, proof)
		if err != nil {
			return false, err
		}
		if contained {
			mr.State, mr.MergedSHA, mr.Ready, mr.Blockers = "merged", proof.MergeSHA, false, nil
			return true, nil
		}
	}
	return false, nil
}
