package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func TestUpdate_RemoteBranchConflictMsg_ShowsDialog(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24

	msg := modal.RemoteBranchConflictMsg{
		TaskID:      "TASK-123",
		ServiceName: "service-a",
		BranchName:  "feature/TASK-123",
		RepoPath:    "/path/to/repo",
	}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.modal == nil {
		t.Error("Expected modal to be set, got nil")
	}
	dialog, ok := m.modal.(*modal.RemoteBranchConflictDialog)
	if !ok {
		t.Errorf("Expected *RemoteBranchConflictDialog, got %T", m.modal)
	}
	if dialog == nil {
		t.Fatal("dialog is nil")
	}
	if dialog.Title() != "Remote Branch Conflict" {
		t.Errorf("Expected title 'Remote Branch Conflict', got %q", dialog.Title())
	}
}

func TestUpdate_SubmitRemoteBranchStrategyMsg_StrategyCancel(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24

	m.pendingInitParams = &task.InitParams{
		TaskID:       "TASK-123",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}

	msg := modal.SubmitRemoteBranchStrategyMsg{
		TaskID:       "TASK-123",
		ServiceName:  "service-a",
		Strategy:     task.StrategyCancel,
		BranchSuffix: "",
	}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.modal != nil {
		t.Error("Expected modal to be nil after cancel")
	}

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil after cancel")
	}
	if m.pendingAddParams != nil {
		t.Error("Expected pendingAddParams to be nil after cancel")
	}
}

func TestUpdate_SubmitRemoteBranchStrategyMsg_StrategyFetchAndSwitch(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24

	m.pendingInitParams = &task.InitParams{
		TaskID:       "TASK-123",
		Services:     []string{"service-a", "service-b"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}

	msg := modal.SubmitRemoteBranchStrategyMsg{
		TaskID:       "TASK-123",
		ServiceName:  "service-a",
		Strategy:     task.StrategyFetchAndSwitch,
		BranchSuffix: "",
	}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.pendingInitParams == nil {
		t.Fatal("pendingInitParams is nil")
	}
	if m.pendingInitParams.RemoteBranchStrategies == nil {
		t.Fatal("RemoteBranchStrategies map is nil")
	}
	strategy, ok := m.pendingInitParams.RemoteBranchStrategies["service-a"]
	if !ok {
		t.Fatal("service-a not in RemoteBranchStrategies map")
	}
	if strategy != task.StrategyFetchAndSwitch {
		t.Errorf("Expected StrategyFetchAndSwitch, got %v", strategy)
	}
}

func TestUpdate_SubmitRemoteBranchStrategyMsg_StrategyNewBranch(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24

	m.pendingInitParams = &task.InitParams{
		TaskID:       "TASK-123",
		Services:     []string{"service-a", "service-b"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}

	msg := modal.SubmitRemoteBranchStrategyMsg{
		TaskID:       "TASK-123",
		ServiceName:  "service-a",
		Strategy:     task.StrategyNewBranch,
		BranchSuffix: "-v2",
	}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.pendingInitParams == nil {
		t.Fatal("pendingInitParams is nil")
	}
	if m.pendingInitParams.RemoteBranchStrategies == nil {
		t.Fatal("RemoteBranchStrategies map is nil")
	}
	strategy, ok := m.pendingInitParams.RemoteBranchStrategies["service-a"]
	if !ok {
		t.Fatal("service-a not in RemoteBranchStrategies map")
	}
	if strategy != task.StrategyNewBranch {
		t.Errorf("Expected StrategyNewBranch, got %v", strategy)
	}
	if m.pendingInitParams.BranchSuffixes == nil {
		t.Fatal("BranchSuffixes map is nil")
	}
	suffix, ok := m.pendingInitParams.BranchSuffixes["service-a"]
	if !ok {
		t.Fatal("service-a not in BranchSuffixes map")
	}
	if suffix != "-v2" {
		t.Errorf("Expected suffix '-v2', got %q", suffix)
	}
}

func TestUpdate_SubmitRemoteBranchStrategyMsg_AddParams(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24

	m.pendingAddParams = &task.AddParams{
		TaskID:   "TASK-123",
		Services: []string{"service-a", "service-b"},
	}

	msg := modal.SubmitRemoteBranchStrategyMsg{
		TaskID:       "TASK-123",
		ServiceName:  "service-a",
		Strategy:     task.StrategyFetchAndSwitch,
		BranchSuffix: "",
	}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.pendingAddParams == nil {
		t.Fatal("pendingAddParams is nil")
	}
	if m.pendingAddParams.RemoteBranchStrategies == nil {
		t.Fatal("RemoteBranchStrategies map is nil")
	}
	strategy, ok := m.pendingAddParams.RemoteBranchStrategies["service-a"]
	if !ok {
		t.Fatal("service-a not in RemoteBranchStrategies map")
	}
	if strategy != task.StrategyFetchAndSwitch {
		t.Errorf("Expected StrategyFetchAndSwitch, got %v", strategy)
	}
}

func TestUpdate_CommandDoneMsg_RemoteBranchConflict(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24
	m.opRunning = true

	m.pendingInitParams = &task.InitParams{
		TaskID:       "TASK-123",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}

	conflictErr := &task.ErrRemoteBranchConflict{
		TaskID:      "TASK-123",
		ServiceName: "service-a",
		BranchName:  "feature/TASK-123",
		RepoPath:    "/path/to/repo",
	}
	msg := CommandDoneMsg{Op: "Init task TASK-123", Err: conflictErr}
	newModel, cmd := m.Update(msg)
	m = newModel.(Model)

	if m.opRunning {
		t.Error("Expected opRunning to be false")
	}

	if m.pendingInitParams == nil {
		t.Error("Expected pendingInitParams to still be set")
	}
	if !strings.Contains(m.outputPanel.View(), "Init task TASK-123: remote branch conflict for service-a") {
		t.Fatalf("expected contextual remote branch conflict log, got %q", m.outputPanel.View())
	}

	if cmd == nil {
		t.Fatal("Expected command to be non-nil")
	}
	result := cmd()
	conflictMsg, ok := result.(modal.RemoteBranchConflictMsg)
	if !ok {
		t.Fatalf("Expected RemoteBranchConflictMsg, got %T", result)
	}
	if conflictMsg.TaskID != "TASK-123" {
		t.Errorf("Expected TaskID 'TASK-123', got %q", conflictMsg.TaskID)
	}
	if conflictMsg.ServiceName != "service-a" {
		t.Errorf("Expected ServiceName 'service-a', got %q", conflictMsg.ServiceName)
	}

	newModel, _ = m.Update(conflictMsg)
	m = newModel.(Model)

	if m.modal == nil {
		t.Error("Expected modal to be set")
	}
	dialog, ok := m.modal.(*modal.RemoteBranchConflictDialog)
	if !ok {
		t.Errorf("Expected *RemoteBranchConflictDialog, got %T", m.modal)
		return
	}
	if dialog == nil {
		t.Fatal("dialog is nil")
	}
}

func TestUpdate_CommandDoneMsg_OtherError_ClearsPendingParams(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24
	m.opRunning = true

	m.pendingInitParams = &task.InitParams{
		TaskID:       "TASK-123",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}

	msg := CommandDoneMsg{Err: errors.New("some other error")}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil after error")
	}
}

func TestUpdate_CommandDoneMsg_Success_ClearsPendingParams(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24
	m.opRunning = true

	m.pendingInitParams = &task.InitParams{
		TaskID:       "TASK-123",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}

	msg := CommandDoneMsg{Err: nil}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil after success")
	}
}

func TestUpdate_CloseModalMsg_ClearsPendingParams(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24

	m.pendingInitParams = &task.InitParams{
		TaskID:       "TASK-123",
		Services:     []string{"service-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}

	msg := modal.CloseModalMsg{}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil after CloseModalMsg")
	}
	if m.pendingAddParams != nil {
		t.Error("Expected pendingAddParams to be nil after CloseModalMsg")
	}
}

func TestUpdate_SubmitInitMsg_StoresPendingParams(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24

	msg := modal.SubmitInitMsg{
		TaskID:       "TASK-123",
		Services:     []string{"service-a", "service-b"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.pendingInitParams == nil {
		t.Fatal("Expected pendingInitParams to be set")
	}
	if m.pendingInitParams.TaskID != "TASK-123" {
		t.Errorf("Expected TaskID 'TASK-123', got %q", m.pendingInitParams.TaskID)
	}
	if len(m.pendingInitParams.Services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(m.pendingInitParams.Services))
	}
	if m.pendingInitParams.BranchPrefix != "feature/" {
		t.Errorf("Expected BranchPrefix 'feature/', got %q", m.pendingInitParams.BranchPrefix)
	}
	if m.pendingInitParams.BaseBranch != "main" {
		t.Errorf("Expected BaseBranch 'main', got %q", m.pendingInitParams.BaseBranch)
	}

	if m.pendingAddParams != nil {
		t.Error("Expected pendingAddParams to be nil")
	}
}

func TestUpdate_SubmitAddMsg_StoresPendingParams(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m.ready = true
	m.width = 80
	m.height = 24

	msg := modal.SubmitAddMsg{
		TaskID:   "TASK-123",
		Services: []string{"service-a", "service-b"},
	}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.pendingAddParams == nil {
		t.Fatal("Expected pendingAddParams to be set")
	}
	if m.pendingAddParams.TaskID != "TASK-123" {
		t.Errorf("Expected TaskID 'TASK-123', got %q", m.pendingAddParams.TaskID)
	}
	if len(m.pendingAddParams.Services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(m.pendingAddParams.Services))
	}

	if m.pendingInitParams != nil {
		t.Error("Expected pendingInitParams to be nil")
	}
}
