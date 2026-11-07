//go:build !windows

package main

// ownsConsole is Windows-specific: elsewhere Envry is always started from a
// terminal or a service manager, never with a window of its own.
func ownsConsole() bool { return false }

func openBrowser(string) error { return nil }
