package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidLibrariesDoNotAuthenticate(t *testing.T) {
	for _, command := range []string{"browse", "lock", "unlock"} {
		for _, fixture := range []struct {
			name, filename, contents, want string
		}{
			{"missing", "", "", "no such file"},
			{"empty", "", "", "no videos"},
			{"metadata-only", ".DS_Store", "metadata", "no videos"},
			{"unrelated", ".CFUserTextEncoding", "0:0", "non-video file"},
			{"invalid-vault", "broken.vlt", "broken", "invalid vault header"},
		} {
			t.Run(command+"/"+fixture.name, func(t *testing.T) {
				root := t.TempDir()
				if fixture.name == "missing" {
					root = filepath.Join(root, "missing")
				}
				if fixture.filename != "" {
					if err := os.WriteFile(filepath.Join(root, fixture.filename), []byte(fixture.contents), 0600); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{root}
				if command != "browse" {
					args = append([]string{command}, args...)
				}
				err := runWithAuthentication(args, func() error {
					t.Fatal("invalid library must fail before authentication")
					return nil
				})
				if err == nil || !strings.Contains(err.Error(), fixture.want) {
					t.Fatalf("got error %v, want %s", err, fixture.want)
				}
			})
		}
	}
}

func TestAuthenticationBeforeLibraryOperations(t *testing.T) {
	denied := errors.New("authentication cancelled")
	for _, command := range []string{"browse", "lock", "unlock"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			name, data := "video.mp4", []byte("video content")
			if command == "unlock" {
				name = "video.vlt"
				var file bytes.Buffer
				if err := writeHeader(&file, header{name: "video.mp4"}); err != nil {
					t.Fatal(err)
				}
				data = file.Bytes()
			}
			path := filepath.Join(root, name)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{root}
			if command != "browse" {
				args = append([]string{command}, args...)
			}
			calls := 0
			err := runWithAuthentication(args, func() error { calls++; return denied })
			if !errors.Is(err, denied) || calls != 1 {
				t.Fatalf("got err=%v, authentication calls=%d", err, calls)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, data) {
				t.Fatal("authentication failure changed the video")
			}
			files, err := os.ReadDir(root)
			if err != nil || len(files) != 1 {
				t.Fatal("authentication failure created or removed files")
			}
		})
	}
}

func TestUnlockWithoutLockedVideosDoesNotAuthenticate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "video.mp4"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	err := runWithAuthentication([]string{"unlock", root}, func() error {
		t.Fatal("nothing to restore must not authenticate")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "no locked videos") {
		t.Fatalf("unexpected error: %v", err)
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
