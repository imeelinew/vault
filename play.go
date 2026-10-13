package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type videoReader struct {
	f   *os.File
	h   header
	pos int64
}

func (r *videoReader) Read(p []byte) (int, error) {
	if r.pos >= r.h.size {
		return 0, io.EOF
	}
	if int64(len(p)) > r.h.size-r.pos {
		p = p[:r.h.size-r.pos]
	}
	n, err := r.f.ReadAt(p, r.h.offset+r.pos)
	if n > 0 {
		block, _ := aes.NewCipher(formatKey[:])
		iv := r.h.iv
		blocks := uint64(r.pos / aes.BlockSize)
		for i := len(iv) - 1; blocks > 0 && i >= 0; i-- {
			blocks += uint64(iv[i])
			iv[i] = byte(blocks)
			blocks >>= 8
		}
		stream := cipher.NewCTR(block, iv[:])
		skip := int(r.pos % aes.BlockSize)
		if skip > 0 {
			var pad [aes.BlockSize]byte
			stream.XORKeyStream(pad[:skip], pad[:skip])
		}
		stream.XORKeyStream(p[:n], p[:n])
		r.pos += int64(n)
	}
	return n, err
}

func (r *videoReader) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.pos + offset
	case io.SeekEnd:
		next = r.h.size + offset
	default:
		return 0, errors.New("invalid seek")
	}
	if next < 0 {
		return 0, errors.New("negative seek")
	}
	r.pos = next
	return next, nil
}

func play(e entry) error {
	iina, err := exec.LookPath("iina")
	if err != nil {
		return errors.New("IINA is required (install IINA and its command-line tool)")
	}
	if !e.locked {
		cmd := exec.Command(iina, "--no-stdin", "--separate-windows", "--keep-running", e.path)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	f, err := os.Open(e.path)
	if err != nil {
		return err
	}
	defer f.Close()
	h, err := readHeader(f)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != h.offset+h.size {
		return errors.New("invalid vault size")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	var token [24]byte
	if _, err = rand.Read(token[:]); err != nil {
		return err
	}
	path := "/" + hex.EncodeToString(token[:]) + filepath.Ext(h.name)
	server := &http.Server{Handler: videoHandler(f, h, path), ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	url := "http://" + listener.Addr().String() + path
	cmd := exec.Command(iina, "--no-stdin", "--separate-windows", "--keep-running", "--mpv-force-media-title="+h.name, url)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("IINA: %w", err)
	}
	return nil
}

func videoHandler(f *os.File, h header, path string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != path || req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", videoType(filepath.Ext(h.name)))
		reader := &videoReader{f: f, h: h}
		http.ServeContent(w, req, h.name, time.Time{}, reader)
	})
}

func videoType(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}
