package main

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNestedRoundTripAndRange(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "private", "more")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	name := "秘密 video 🐈.mkv"
	plain := filepath.Join(nested, name)
	data := make([]byte, 2<<20+17)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	items, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d videos", len(items))
	}
	if err := process(items, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatalf("plaintext remains: %v", err)
	}
	items, err = scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].locked || items[0].name != name {
		t.Fatalf("bad locked scan: %+v", items)
	}
	lockedBytes, err := os.ReadFile(items[0].path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(lockedBytes, []byte(name)) || bytes.Contains(lockedBytes, data[100:140]) {
		t.Fatal("plaintext visible in vault file")
	}
	f, err := os.Open(items[0].path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := readHeader(f)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/secret.mkv", nil)
	request.Header.Set("Range", "bytes=65537-66536")
	response := httptest.NewRecorder()
	videoHandler(f, h, "/secret.mkv").ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || !bytes.Equal(response.Body.Bytes(), data[65537:66537]) {
		t.Fatalf("HTTP range failed: status %d", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	for _, test := range []struct {
		off int64
		n   int
	}{{0, 100}, {13, 200}, {65537, 8000}, {int64(len(data)) - 37, 37}} {
		r := &videoReader{f: f, h: h}
		if _, err := r.Seek(test.off, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, test.n)
		if _, err := io.ReadFull(r, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data[test.off:test.off+int64(test.n)]) {
			t.Fatalf("range mismatch at %d", test.off)
		}
	}
	f.Close()
	if err := process(items, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestInvalidFilesAndChecksum(t *testing.T) {
	root := t.TempDir()
	json := filepath.Join(root, "data.json")
	if err := os.WriteFile(json, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scan(root); err == nil || !strings.Contains(err.Error(), "non-video") {
		t.Fatalf("expected non-video error: %v", err)
	}
	os.Remove(json)
	plain := filepath.Join(root, "video.mp4")
	if err := os.WriteFile(plain, bytes.Repeat([]byte("video"), 100), 0600); err != nil {
		t.Fatal(err)
	}
	items, _ := scan(root)
	if err := process(items, false); err != nil {
		t.Fatal(err)
	}
	items, _ = scan(root)
	f, err := os.OpenFile(items[0].path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := readHeader(f)
	if _, err := f.WriteAt([]byte{0xff}, h.offset+10); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := process(items, true); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum error: %v", err)
	}
	if _, err := os.Stat(items[0].path); err != nil {
		t.Fatal("vault file lost", err)
	}
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatal("corrupt plaintext published")
	}
}
