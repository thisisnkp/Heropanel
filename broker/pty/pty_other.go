//go:build !linux

package pty

import "errors"

// ErrUnsupported is returned when a PTY is requested on a platform this broker
// build does not implement. HeroPanel targets Linux; this stub exists so the
// repository still compiles (and its tests still run) on a developer's machine.
var ErrUnsupported = errors.New("pty: pseudo-terminals are only supported on Linux")

// Session is the non-Linux placeholder. It is never constructed.
type Session struct{}

// Start always fails on non-Linux platforms.
func Start(Config) (*Session, error) { return nil, ErrUnsupported }

func (s *Session) Read([]byte) (int, error)    { return 0, ErrUnsupported }
func (s *Session) Write([]byte) (int, error)   { return 0, ErrUnsupported }
func (s *Session) Resize(uint16, uint16) error { return ErrUnsupported }
func (s *Session) InputVisible() bool          { return true }
func (s *Session) Wait() int                   { return -1 }
func (s *Session) Close() error                { return nil }
