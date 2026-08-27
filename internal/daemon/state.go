package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const maxStateSize = 4 << 20
const recoveryMarkerSuffix = ".recovery"

type usageState struct {
	Date              string                      `json:"date"`
	DeviceSeconds     map[string]int64            `json:"device_seconds"`
	ContinuousSeconds map[string]int64            `json:"continuous_seconds"`
	BreakUntil        map[string]time.Time        `json:"break_until"`
	Users             map[string]map[string]int64 `json:"users"`
}

func newState(date string) usageState {
	return usageState{Date: date, DeviceSeconds: make(map[string]int64), ContinuousSeconds: make(map[string]int64), BreakUntil: make(map[string]time.Time), Users: make(map[string]map[string]int64)}
}

func loadServiceState(path, date string) (usageState, *stateRecovery) {
	markerReset, markerPresent, markerErr := loadRecoveryMarker(path)
	state, stateErr := loadState(path, date)
	if markerErr != nil {
		return newState(date), &stateRecovery{reason: fmt.Errorf("invalid recovery marker: %w", markerErr), resetRequired: true}
	}
	if markerPresent {
		if stateErr != nil {
			state = newState(date)
			markerReset = true
		}
		return state, &stateRecovery{reason: errors.New("an earlier usage state recovery did not complete"), resetRequired: markerReset}
	}
	if stateErr != nil {
		return newState(date), &stateRecovery{reason: stateErr, resetRequired: true}
	}
	return state, nil
}

func recoveryMarkerPath(path string) string { return path + recoveryMarkerSuffix }

func loadRecoveryMarker(statePath string) (resetRequired, present bool, err error) {
	path := recoveryMarkerPath(statePath)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if err := validatePrivateFile(path, info); err != nil {
		return false, true, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, true, err
	}
	switch string(bytes.TrimSpace(data)) {
	case "reset":
		return true, true, nil
	case "retry":
		return false, true, nil
	default:
		return false, true, errors.New("unknown recovery marker mode")
	}
}

func writeRecoveryMarker(statePath string, resetRequired bool) error {
	directory := filepath.Dir(statePath)
	if err := validatePrivateDirectory(directory); err != nil {
		return err
	}
	mode := "retry\n"
	if resetRequired {
		mode = "reset\n"
	}
	path := recoveryMarkerPath(statePath)
	if existingReset, present, err := loadRecoveryMarker(statePath); err == nil && present && (existingReset || !resetRequired) {
		return nil
	}
	return writePrivateFile(path, []byte(mode))
}

func writePrivateFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	tmp, err := os.CreateTemp(directory, ".recovery-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func (s *Service) recoverState() (string, error) {
	if err := writeRecoveryMarker(s.statePath, s.recovery.resetRequired); err != nil {
		return "", fmt.Errorf("create recovery marker: %w", err)
	}
	quarantine := ""
	if s.recovery.resetRequired {
		var err error
		quarantine, err = quarantineState(s.statePath, s.now().UTC())
		if err != nil {
			return "", err
		}
		s.state = newState(s.now().In(s.cfg.Location()).Format("2006-01-02"))
	}
	if err := saveState(s.statePath, s.state); err != nil {
		return "", fmt.Errorf("write recovered state: %w", err)
	}
	if err := os.Remove(recoveryMarkerPath(s.statePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove recovery marker: %w", err)
	}
	if err := syncDirectory(filepath.Dir(s.statePath)); err != nil {
		return "", err
	}
	s.recovery = nil
	s.lastTick = s.now()
	s.previousUsers = make(map[string]bool)
	s.previousApps = make(map[string]map[string]bool)
	if quarantine != "" {
		return fmt.Sprintf("usage state reset; invalid state preserved at %s", quarantine), nil
	}
	return "usage state persistence restored", nil
}

func (s *Service) retryStatePersistence() error {
	if err := saveState(s.statePath, s.state); err != nil {
		s.recovery.reason = err
		return err
	}
	if err := os.Remove(recoveryMarkerPath(s.statePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.recovery.reason = err
		return err
	}
	if err := syncDirectory(filepath.Dir(s.statePath)); err != nil {
		s.recovery.reason = err
		return err
	}
	s.recovery = nil
	s.lastTick = s.now()
	s.previousUsers = make(map[string]bool)
	s.previousApps = make(map[string]map[string]bool)
	return nil
}

func quarantineState(path string, now time.Time) (string, error) {
	if err := validatePrivateDirectory(filepath.Dir(path)); err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	base := fmt.Sprintf("%s.invalid-%s", path, now.Format("20060102T150405Z"))
	target := base
	for suffix := 2; ; suffix++ {
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return "", err
		}
		target = fmt.Sprintf("%s-%d", base, suffix)
	}
	if err := os.Rename(path, target); err != nil {
		return "", fmt.Errorf("preserve invalid state: %w", err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return "", err
	}
	return target, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func loadState(path, date string) (usageState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return newState(date), nil
	}
	if err != nil {
		return usageState{}, err
	}
	if err := validatePrivateFile(path, info); err != nil {
		return usageState{}, err
	}
	if info.Size() > maxStateSize {
		return usageState{}, fmt.Errorf("state exceeds %d bytes", maxStateSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return usageState{}, err
	}
	var state usageState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return usageState{}, fmt.Errorf("decode state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return usageState{}, errors.New("state contains trailing data")
	}
	if _, err := time.Parse("2006-01-02", state.Date); err != nil {
		return usageState{}, fmt.Errorf("invalid state date: %w", err)
	}
	for username, applications := range state.Users {
		for application, seconds := range applications {
			if seconds < 0 || seconds > 86400 {
				return usageState{}, fmt.Errorf("invalid usage for %q/%q: %d", username, application, seconds)
			}
		}
	}
	for username, seconds := range state.DeviceSeconds {
		if seconds < 0 || seconds > 86400 {
			return usageState{}, fmt.Errorf("invalid device usage for %q: %d", username, seconds)
		}
	}
	for username, seconds := range state.ContinuousSeconds {
		if seconds < 0 || seconds > 86400 {
			return usageState{}, fmt.Errorf("invalid continuous usage for %q: %d", username, seconds)
		}
	}
	for username, until := range state.BreakUntil {
		if until.IsZero() {
			return usageState{}, fmt.Errorf("invalid break deadline for %q", username)
		}
	}
	if state.Date != date {
		return newState(date), nil
	}
	if state.Users == nil {
		state.Users = make(map[string]map[string]int64)
	}
	if state.DeviceSeconds == nil {
		state.DeviceSeconds = make(map[string]int64)
	}
	if state.ContinuousSeconds == nil {
		state.ContinuousSeconds = make(map[string]int64)
	}
	if state.BreakUntil == nil {
		state.BreakUntil = make(map[string]time.Time)
	}
	return state, nil
}

func saveState(path string, state usageState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := validatePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".usage-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("state directory %s must be owned by UID %d with mode 0700", path, os.Geteuid())
	}
	return nil
}

func validatePrivateFile(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return fmt.Errorf("state %s must be a regular UID %d-owned file with mode 0600", path, os.Geteuid())
	}
	return nil
}
