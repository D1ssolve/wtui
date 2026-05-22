package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func TestE2E_Init_RemoteConflict_FetchAndSwitch(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)

	m.repos = []domain.Repo{{Name: "service-a", Path: "/tmp/repos/service-a"}}

	t.Log("Step 1: User opens Init dialog")
	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("Expected InitDialog to be open")
	}

	t.Log("Step 2: User submits Init dialog")
	submitMsg := modal.SubmitInitMsg{
		TaskID:       "TASK-123",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}
	updated, _ = m.Update(submitMsg)
	m = updated.(Model)

	if m.pendingInitParams == nil {
		t.Fatal("Expected pendingInitParams to be set after SubmitInitMsg")
	}
	if m.pendingInitParams.TaskID != "TASK-123" {
		t.Errorf("Expected TaskID 'TASK-123', got %q", m.pendingInitParams.TaskID)
	}

	t.Log("Step 3: Simulate CommandDoneMsg with conflict error")
	conflictErr := &task.ErrRemoteBranchConflict{
		TaskID:      "TASK-123",
		ServiceName: "service-a",
		BranchName:  "feature/TASK-123",
		RepoPath:    "/tmp/repos/service-a",
	}
	doneMsg := CommandDoneMsg{Err: conflictErr}

	updated, conflictCmd := m.Update(doneMsg)
	m = updated.(Model)

	if m.opRunning {
		t.Error("Expected opRunning to be false after CommandDoneMsg")
	}

	t.Log("Step 4: Process RemoteBranchConflictMsg")
	if conflictCmd == nil {
		t.Fatal("Expected conflictCmd to be returned")
	}
	conflictMsgResult := conflictCmd()
	conflictMsg, ok := conflictMsgResult.(modal.RemoteBranchConflictMsg)
	if !ok {
		t.Fatalf("Expected RemoteBranchConflictMsg, got %T", conflictMsgResult)
	}

	if conflictMsg.TaskID != "TASK-123" {
		t.Errorf("Expected TaskID 'TASK-123', got %q", conflictMsg.TaskID)
	}
	if conflictMsg.ServiceName != "service-a" {
		t.Errorf("Expected ServiceName 'service-a', got %q", conflictMsg.ServiceName)
	}
	if conflictMsg.BranchName != "feature/TASK-123" {
		t.Errorf("Expected BranchName 'feature/TASK-123', got %q", conflictMsg.BranchName)
	}

	t.Log("Step 5: Show RemoteBranchConflictDialog")
	updated, _ = m.Update(conflictMsg)
	m = updated.(Model)

	if m.modal == nil {
		t.Fatal("Expected RemoteBranchConflictDialog to be shown")
	}
	dialog, ok := m.modal.(*modal.RemoteBranchConflictDialog)
	if !ok {
		t.Fatalf("Expected *RemoteBranchConflictDialog, got %T", m.modal)
	}

	t.Log("Step 6: Verify default selection is Track Remote Branch")

	t.Log("Step 7: User presses Enter to confirm Track Remote Branch")
	updatedModal, submitCmd := dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	if submitCmd == nil {
		t.Fatal("Expected command to be returned after Enter")
	}
	submitResult := submitCmd()
	strategyMsg, ok := submitResult.(modal.SubmitRemoteBranchStrategyMsg)
	if !ok {
		t.Fatalf("Expected SubmitRemoteBranchStrategyMsg, got %T", submitResult)
	}

	if strategyMsg.Strategy != task.StrategyFetchAndSwitch {
		t.Errorf("Expected StrategyFetchAndSwitch, got %v", strategyMsg.Strategy)
	}
	if strategyMsg.TaskID != "TASK-123" {
		t.Errorf("Expected TaskID 'TASK-123', got %q", strategyMsg.TaskID)
	}
	if strategyMsg.ServiceName != "service-a" {
		t.Errorf("Expected ServiceName 'service-a', got %q", strategyMsg.ServiceName)
	}

	t.Log("Step 8: Process SubmitRemoteBranchStrategyMsg")
	updated, _ = m.Update(strategyMsg)
	m = updated.(Model)

	if m.pendingInitParams == nil {
		t.Fatal("Expected pendingInitParams to still be set")
	}
	if m.pendingInitParams.RemoteBranchStrategies == nil {
		t.Fatal("Expected RemoteBranchStrategies map to be initialized")
	}
	strategy, ok := m.pendingInitParams.RemoteBranchStrategies["service-a"]
	if !ok {
		t.Fatal("Expected 'service-a' in RemoteBranchStrategies map")
	}
	if strategy != task.StrategyFetchAndSwitch {
		t.Errorf("Expected StrategyFetchAndSwitch, got %v", strategy)
	}

	if m.modal != nil {
		t.Error("Expected modal to be closed after strategy selection")
	}

	t.Log("E2E test completed: Track Remote Branch workflow verified")
}

func TestE2E_Init_RemoteConflict_NewBranchWithSuffix(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.repos = []domain.Repo{{Name: "service-a", Path: "/tmp/repos/service-a"}}

	t.Log("Step 1: User opens and submits Init dialog")
	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)

	submitMsg := modal.SubmitInitMsg{
		TaskID:       "TASK-456",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}
	updated, _ = m.Update(submitMsg)
	m = updated.(Model)

	t.Log("Step 2: Simulate CommandDoneMsg with conflict")
	conflictErr := &task.ErrRemoteBranchConflict{
		TaskID:      "TASK-456",
		ServiceName: "service-a",
		BranchName:  "feature/TASK-456",
		RepoPath:    "/tmp/repos/service-a",
	}
	doneMsg := CommandDoneMsg{Err: conflictErr}

	updated, conflictCmd := m.Update(doneMsg)
	m = updated.(Model)

	t.Log("Step 3: Process RemoteBranchConflictMsg")
	conflictMsgResult := conflictCmd()
	conflictMsg := conflictMsgResult.(modal.RemoteBranchConflictMsg)

	updated, _ = m.Update(conflictMsg)
	m = updated.(Model)

	dialog, ok := m.modal.(*modal.RemoteBranchConflictDialog)
	if !ok {
		t.Fatalf("Expected *RemoteBranchConflictDialog, got %T", m.modal)
	}

	t.Log("Step 4: User navigates to 'New Branch' option")

	updatedModal, _ := dialog.Update(sendKey("j"))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	t.Log("Step 5: User presses Enter to enter suffix mode")
	updatedModal, enterCmd := dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	if enterCmd != nil {
		t.Fatal("Enter on 'New Branch' should not emit a message yet")
	}

	t.Log("Step 6: User types suffix '-v2'")
	for _, char := range "-v2" {
		updatedModal, _ = dialog.Update(sendKey(string(char)))
		dialog = updatedModal.(*modal.RemoteBranchConflictDialog)
	}

	t.Log("Step 7: User presses Enter to confirm suffix")
	updatedModal, submitCmd := dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	if submitCmd == nil {
		t.Fatal("Expected command to be returned after Enter with valid suffix")
	}

	submitResult := submitCmd()
	strategyMsg, ok := submitResult.(modal.SubmitRemoteBranchStrategyMsg)
	if !ok {
		t.Fatalf("Expected SubmitRemoteBranchStrategyMsg, got %T", submitResult)
	}

	if strategyMsg.Strategy != task.StrategyNewBranch {
		t.Errorf("Expected StrategyNewBranch, got %v", strategyMsg.Strategy)
	}
	if strategyMsg.BranchSuffix != "-v2" {
		t.Errorf("Expected BranchSuffix '-v2', got %q", strategyMsg.BranchSuffix)
	}
	if strategyMsg.TaskID != "TASK-456" {
		t.Errorf("Expected TaskID 'TASK-456', got %q", strategyMsg.TaskID)
	}

	t.Log("Step 8: Process SubmitRemoteBranchStrategyMsg")
	updated, _ = m.Update(strategyMsg)
	m = updated.(Model)

	if m.pendingInitParams == nil {
		t.Fatal("Expected pendingInitParams to still be set")
	}
	if m.pendingInitParams.RemoteBranchStrategies == nil {
		t.Fatal("Expected RemoteBranchStrategies map to be initialized")
	}
	strategy, ok := m.pendingInitParams.RemoteBranchStrategies["service-a"]
	if !ok {
		t.Fatal("Expected 'service-a' in RemoteBranchStrategies map")
	}
	if strategy != task.StrategyNewBranch {
		t.Errorf("Expected StrategyNewBranch, got %v", strategy)
	}
	if m.pendingInitParams.BranchSuffixes == nil {
		t.Fatal("Expected BranchSuffixes map to be initialized")
	}
	suffix, ok := m.pendingInitParams.BranchSuffixes["service-a"]
	if !ok {
		t.Fatal("Expected 'service-a' in BranchSuffixes map")
	}
	if suffix != "-v2" {
		t.Errorf("Expected suffix '-v2', got %q", suffix)
	}

	if m.modal != nil {
		t.Error("Expected modal to be closed after strategy selection")
	}

	t.Log("E2E test completed: New Branch with suffix workflow verified")
}

func TestE2E_Init_RemoteConflict_Cancel(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.repos = []domain.Repo{{Name: "service-a", Path: "/tmp/repos/service-a"}}

	t.Log("Step 1: User opens and submits Init dialog")
	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)

	submitMsg := modal.SubmitInitMsg{
		TaskID:       "TASK-789",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}
	updated, _ = m.Update(submitMsg)
	m = updated.(Model)

	t.Log("Step 2: Simulate CommandDoneMsg with conflict")
	conflictErr := &task.ErrRemoteBranchConflict{
		TaskID:      "TASK-789",
		ServiceName: "service-a",
		BranchName:  "feature/TASK-789",
		RepoPath:    "/tmp/repos/service-a",
	}
	doneMsg := CommandDoneMsg{Err: conflictErr}

	updated, conflictCmd := m.Update(doneMsg)
	m = updated.(Model)

	t.Log("Step 3: Process RemoteBranchConflictMsg")
	conflictMsgResult := conflictCmd()
	conflictMsg := conflictMsgResult.(modal.RemoteBranchConflictMsg)

	updated, _ = m.Update(conflictMsg)
	m = updated.(Model)

	dialog, ok := m.modal.(*modal.RemoteBranchConflictDialog)
	if !ok {
		t.Fatalf("Expected *RemoteBranchConflictDialog, got %T", m.modal)
	}

	t.Log("Step 4: User navigates to 'Cancel' option")

	updatedModal, _ := dialog.Update(sendKey("j"))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)
	updatedModal, _ = dialog.Update(sendKey("j"))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	t.Log("Step 5: User presses Enter to cancel")
	updatedModal, submitCmd := dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	if submitCmd == nil {
		t.Fatal("Expected command to be returned after Enter")
	}

	submitResult := submitCmd()
	strategyMsg, ok := submitResult.(modal.SubmitRemoteBranchStrategyMsg)
	if !ok {
		t.Fatalf("Expected SubmitRemoteBranchStrategyMsg, got %T", submitResult)
	}
	if strategyMsg.Strategy != task.StrategyCancel {
		t.Errorf("Expected StrategyCancel, got %v", strategyMsg.Strategy)
	}

	t.Log("Step 6: Process SubmitRemoteBranchStrategyMsg with Cancel")
	updated, _ = m.Update(strategyMsg)
	m = updated.(Model)

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil after cancel")
	}

	if m.modal != nil {
		t.Error("Expected modal to be nil after cancel")
	}

	t.Log("E2E test completed: Cancel workflow verified")
}

func TestE2E_Init_NoRemoteConflict_NormalFlow(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.repos = []domain.Repo{{Name: "service-a", Path: "/tmp/repos/service-a"}}

	t.Log("Step 1: User opens and submits Init dialog")
	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)

	submitMsg := modal.SubmitInitMsg{
		TaskID:       "TASK-000",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}
	updated, _ = m.Update(submitMsg)
	m = updated.(Model)

	if m.pendingInitParams == nil {
		t.Fatal("Expected pendingInitParams to be set after SubmitInitMsg")
	}

	t.Log("Step 2: Simulate CommandDoneMsg with no error")
	doneMsg := CommandDoneMsg{Err: nil}

	updated, _ = m.Update(doneMsg)
	m = updated.(Model)

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil after successful init")
	}

	if m.modal != nil {
		t.Error("Expected modal to be nil after successful init")
	}

	if m.opRunning {
		t.Error("Expected opRunning to be false after CommandDoneMsg")
	}

	t.Log("E2E test completed: Normal flow (no conflict) verified")
}

func TestE2E_Init_RemoteConflict_EscCancels(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.repos = []domain.Repo{{Name: "service-a", Path: "/tmp/repos/service-a"}}

	t.Log("Step 1: User opens and submits Init dialog")
	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)

	submitMsg := modal.SubmitInitMsg{
		TaskID:       "TASK-ESC",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}
	updated, _ = m.Update(submitMsg)
	m = updated.(Model)

	t.Log("Step 2: Simulate CommandDoneMsg with conflict")
	conflictErr := &task.ErrRemoteBranchConflict{
		TaskID:      "TASK-ESC",
		ServiceName: "service-a",
		BranchName:  "feature/TASK-ESC",
		RepoPath:    "/tmp/repos/service-a",
	}
	doneMsg := CommandDoneMsg{Err: conflictErr}

	updated, conflictCmd := m.Update(doneMsg)
	m = updated.(Model)

	t.Log("Step 3: Process RemoteBranchConflictMsg")
	conflictMsgResult := conflictCmd()
	conflictMsg := conflictMsgResult.(modal.RemoteBranchConflictMsg)

	updated, _ = m.Update(conflictMsg)
	m = updated.(Model)

	dialog, ok := m.modal.(*modal.RemoteBranchConflictDialog)
	if !ok {
		t.Fatalf("Expected *RemoteBranchConflictDialog, got %T", m.modal)
	}

	t.Log("Step 4: User presses Esc to cancel")
	updatedModal, escCmd := dialog.Update(sendSpecialKey(KeyEsc))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	if escCmd == nil {
		t.Fatal("Expected command to be returned after Esc")
	}

	escResult := escCmd()
	_, ok = escResult.(modal.CloseModalMsg)
	if !ok {
		t.Fatalf("Expected CloseModalMsg, got %T", escResult)
	}

	t.Log("Step 5: Process CloseModalMsg")
	updated, _ = m.Update(modal.CloseModalMsg{})
	m = updated.(Model)

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil after Esc cancel")
	}

	if m.modal != nil {
		t.Error("Expected modal to be nil after Esc cancel")
	}

	t.Log("E2E test completed: Esc cancel workflow verified")
}

func TestE2E_Init_RemoteConflict_MultipleServices(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.repos = []domain.Repo{
		{Name: "service-a", Path: "/tmp/repos/service-a"},
		{Name: "service-b", Path: "/tmp/repos/service-b"},
	}

	t.Log("Step 1: User opens and submits Init dialog with multiple services")
	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)

	submitMsg := modal.SubmitInitMsg{
		TaskID:       "TASK-MULTI",
		Services:     []string{"service-a", "service-b"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}
	updated, _ = m.Update(submitMsg)
	m = updated.(Model)

	if m.pendingInitParams == nil {
		t.Fatal("Expected pendingInitParams to be set")
	}
	if len(m.pendingInitParams.Services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(m.pendingInitParams.Services))
	}

	t.Log("Step 2: Simulate CommandDoneMsg with conflict for first service")
	conflictErr := &task.ErrRemoteBranchConflict{
		TaskID:      "TASK-MULTI",
		ServiceName: "service-a",
		BranchName:  "feature/TASK-MULTI",
		RepoPath:    "/tmp/repos/service-a",
	}
	doneMsg := CommandDoneMsg{Err: conflictErr}

	updated, conflictCmd := m.Update(doneMsg)
	m = updated.(Model)

	t.Log("Step 3: Process RemoteBranchConflictMsg for first service")
	conflictMsgResult := conflictCmd()
	conflictMsg := conflictMsgResult.(modal.RemoteBranchConflictMsg)

	if conflictMsg.ServiceName != "service-a" {
		t.Errorf("Expected first conflict for 'service-a', got %q", conflictMsg.ServiceName)
	}

	updated, _ = m.Update(conflictMsg)
	m = updated.(Model)

	if m.modal == nil {
		t.Fatal("Expected RemoteBranchConflictDialog to be shown")
	}

	t.Log("Step 4: User selects Track Remote Branch for first service")
	dialog := m.modal.(*modal.RemoteBranchConflictDialog)
	updatedModal, submitCmd := dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	submitResult := submitCmd()
	strategyMsg := submitResult.(modal.SubmitRemoteBranchStrategyMsg)

	t.Log("Step 5: Process SubmitRemoteBranchStrategyMsg for first service")
	updated, _ = m.Update(strategyMsg)
	m = updated.(Model)

	if m.pendingInitParams.RemoteBranchStrategies == nil {
		t.Fatal("Expected RemoteBranchStrategies map to be initialized")
	}
	strategy, ok := m.pendingInitParams.RemoteBranchStrategies["service-a"]
	if !ok {
		t.Fatal("Expected 'service-a' in RemoteBranchStrategies map")
	}
	if strategy != task.StrategyFetchAndSwitch {
		t.Errorf("Expected StrategyFetchAndSwitch, got %v", strategy)
	}

	t.Log("E2E test completed: Multiple services workflow verified (first service handled)")
}

func TestE2E_Init_OtherError_ClearsPendingParams(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.repos = []domain.Repo{{Name: "service-a", Path: "/tmp/repos/service-a"}}

	t.Log("Step 1: User opens and submits Init dialog")
	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)

	submitMsg := modal.SubmitInitMsg{
		TaskID:       "TASK-ERR",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}
	updated, _ = m.Update(submitMsg)
	m = updated.(Model)

	t.Log("Step 2: Simulate CommandDoneMsg with a regular error")
	doneMsg := CommandDoneMsg{Err: errors.New("some other error")}

	updated, _ = m.Update(doneMsg)
	m = updated.(Model)

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil after error")
	}

	if m.modal != nil {
		t.Error("Expected modal to be nil after error")
	}

	t.Log("E2E test completed: Other error clears pending params")
}

func TestE2E_Init_RemoteConflict_SuffixInputValidation(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.repos = []domain.Repo{{Name: "service-a", Path: "/tmp/repos/service-a"}}

	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)
	updated, _ = m.Update(modal.SubmitInitMsg{
		TaskID:       "TASK-SUFFIX",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	})
	m = updated.(Model)

	conflictErr := &task.ErrRemoteBranchConflict{
		TaskID:      "TASK-SUFFIX",
		ServiceName: "service-a",
		BranchName:  "feature/TASK-SUFFIX",
		RepoPath:    "/tmp/repos/service-a",
	}
	updated, conflictCmd := m.Update(CommandDoneMsg{Err: conflictErr})
	m = updated.(Model)
	conflictMsg := conflictCmd().(modal.RemoteBranchConflictMsg)
	updated, _ = m.Update(conflictMsg)
	m = updated.(Model)

	dialog := m.modal.(*modal.RemoteBranchConflictDialog)

	updatedModal, _ := dialog.Update(sendKey("j"))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	updatedModal, _ = dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)

	t.Log("Test 1: Empty suffix should fail")
	updatedModal, cmd := dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)
	if cmd != nil {
		t.Error("Empty suffix should not emit a command")
	}

	t.Log("Test 2: Valid suffix should succeed")

	for _, char := range "-v2" {
		updatedModal, _ = dialog.Update(sendKey(string(char)))
		dialog = updatedModal.(*modal.RemoteBranchConflictDialog)
	}

	updatedModal, cmd = dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.RemoteBranchConflictDialog)
	if cmd == nil {
		t.Fatal("Valid suffix should emit a command")
	}

	submitResult := cmd()
	strategyMsg, ok := submitResult.(modal.SubmitRemoteBranchStrategyMsg)
	if !ok {
		t.Fatalf("Expected SubmitRemoteBranchStrategyMsg, got %T", submitResult)
	}
	if strategyMsg.BranchSuffix != "-v2" {
		t.Errorf("Expected BranchSuffix '-v2', got %q", strategyMsg.BranchSuffix)
	}

	t.Log("E2E test completed: Suffix input validation verified")
}

const (
	KeyEnter = "enter"
	KeyEsc   = "escape"
)

func sendSpecialKey(keyType string) tea.Msg {
	switch keyType {
	case KeyEnter:
		return tea.KeyMsg{Type: tea.KeyEnter}
	case KeyEsc:
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{}}
	}
}

func sendKey(key string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
