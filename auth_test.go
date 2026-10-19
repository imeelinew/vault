package main

import (
	"errors"
	"testing"
)

func TestAuthenticationBeforeDirectoryAccess(t *testing.T) {
	denied := errors.New("authentication cancelled")
	for _, command := range []string{"browse", "lock", "unlock"} {
		t.Run(command, func(t *testing.T) {
			args := []string{"/vault-auth-test-does-not-exist"}
			if command != "browse" {
				args = append([]string{command}, args...)
			}
			calls := 0
			err := runWithAuthentication(args, func() error { calls++; return denied })
			if !errors.Is(err, denied) || calls != 1 {
				t.Fatalf("got err=%v, authentication calls=%d", err, calls)
			}
		})
	}
}

func TestHelpDoesNotAuthenticate(t *testing.T) {
	err := runWithAuthentication([]string{"--help"}, func() error {
		t.Fatal("help must not authenticate")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
