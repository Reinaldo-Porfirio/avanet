package autostart

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// Enable registers exe to start at logon, replacing any previous entry.
func (e Entry) Enable(exe string) error {
	command, err := Command(exe)
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, e.KeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open %s: %w", e.KeyPath, err)
	}
	return errors.Join(wrap("write autostart entry", key.SetStringValue(e.ValueName, command)), key.Close())
}

// Disable removes the registration. Removing a missing entry is not an error.
func (e Entry) Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, e.KeyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", e.KeyPath, err)
	}
	err = key.DeleteValue(e.ValueName)
	if errors.Is(err, registry.ErrNotExist) {
		err = nil
	}
	return errors.Join(wrap("delete autostart entry", err), key.Close())
}

// Status returns the registered command line, if any.
func (e Entry) Status() (command string, enabled bool, err error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, e.KeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("open %s: %w", e.KeyPath, err)
	}
	defer func() { err = errors.Join(err, key.Close()) }()

	command, _, err = key.GetStringValue(e.ValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read autostart entry: %w", err)
	}
	return command, true, nil
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
