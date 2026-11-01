//go:build !linux

package main

import "context"

func startTouch(ctx context.Context, home func(), taps chan<- Tap) error { return nil }
