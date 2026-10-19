//go:build !darwin || !cgo

package main

import "errors"

func authenticate() error {
	return errors.New("requires macOS with Touch ID; build with CGO_ENABLED=1")
}
