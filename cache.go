package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const thumbnailCacheDir = ".vault-thumbnails"

var errThumbnailCache = errors.New("preview unavailable; run v lock to build cache")

// Include inode and change time so replacing a file, or editing it while
// restoring its size/mtime, cannot reuse another video's thumbnail.
func thumbnailCachePath(e entry) (string, error) {
	if !e.locked {
		return "", errThumbnailCache
	}
	f, err := os.Open(e.path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	h, err := readHeader(f)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != h.offset+h.size || h.name != e.name || h.size != e.size {
		return "", errors.New("video changed")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("file identity unavailable")
	}
	fingerprint := sha256.New()
	fmt.Fprintf(fingerprint, "vault-thumbnail-v1:%d:%d:%d:%d:%d:%d:%x:%x",
		stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano(),
		stat.Ctimespec.Sec, stat.Ctimespec.Nsec, h.iv, h.digest)
	key := hex.EncodeToString(fingerprint.Sum(nil))
	return filepath.Join(filepath.Dir(e.path), thumbnailCacheDir, key+".thumb"), nil
}

func thumbnailCipher() cipher.AEAD {
	key := sha256.Sum256(append([]byte("vault-thumbnail-cache-v1:"), formatKey[:]...))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	return aead
}

func readThumbnailCache(e entry) ([]byte, error) {
	path, err := thumbnailCachePath(e)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errThumbnailCache
	}
	defer f.Close()
	aead := thumbnailCipher()
	sealed, err := io.ReadAll(io.LimitReader(f, maxPreviewBytes+128))
	if err != nil || len(sealed) < aead.NonceSize()+aead.Overhead() || len(sealed) > maxPreviewBytes+aead.NonceSize()+aead.Overhead() {
		return nil, errThumbnailCache
	}
	data, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(filepath.Base(path)))
	if err != nil {
		return nil, errThumbnailCache
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		clear(data)
		return nil, errThumbnailCache
	}
	// A replacement while reading must not display an old image.
	current, err := thumbnailCachePath(e)
	if err != nil || current != path {
		clear(data)
		return nil, errThumbnailCache
	}
	return data, nil
}

func writeThumbnailCache(e entry) error {
	path, err := thumbnailCachePath(e)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := thumbnail(ctx, e)
	if err != nil {
		return err
	}
	defer clear(data)
	current, err := thumbnailCachePath(e)
	if err != nil || current != path {
		return errors.New("video changed while generating thumbnail")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return errors.New("invalid thumbnail cache directory")
	}
	aead := thumbnailCipher()
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := aead.Seal(nonce, nonce, data, []byte(filepath.Base(path)))
	f, err := os.CreateTemp(dir, ".thumbnail-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(sealed); err != nil {
		f.Close()
		return err
	}
	if err := durableClose(f); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Reconcile by identity, never by the ordering of directory entries. Only
// lock generates images; browsing and unlock remove orphaned cache data.
func maintainThumbnailCache(root string, generate bool) error {
	items, err := scan(root)
	if err != nil {
		return err
	}
	live := make(map[string]bool)
	for _, e := range items {
		if !e.locked {
			continue
		}
		path, err := thumbnailCachePath(e)
		if err != nil {
			return err
		}
		live[path] = true
	}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() || d.Name() != thumbnailCacheDir {
			return nil
		}
		files, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		remaining := 0
		for _, file := range files {
			cached := filepath.Join(path, file.Name())
			if live[cached] {
				remaining++
				continue
			}
			if err := os.RemoveAll(cached); err != nil {
				return err
			}
		}
		if remaining == 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		return filepath.SkipDir
	}); err != nil {
		return err
	}
	if !generate {
		return nil
	}
	var failures []error
	for _, e := range items {
		if !e.locked {
			continue
		}
		data, err := readThumbnailCache(e)
		clear(data)
		if err == nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "Caching preview: %s\n", displayText(e.name))
		if err := writeThumbnailCache(e); err != nil {
			failures = append(failures, fmt.Errorf("thumbnail %s: %w", e.path, err))
		}
	}
	return errors.Join(failures...)
}
