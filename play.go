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
	url, closeStream, err := streamVideo(e)
	if err != nil {
		return err
	}
	defer closeStream()
	cmd := exec.Command(iina, "--no-stdin", "--separate-windows", "--keep-running", "--mpv-force-media-title="+displayText(e.name), url)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("IINA: %w", err)
	}
	return nil
}

// Both consumers use the same short-lived loopback stream. Plaintext exists
// only in request buffers; even seek-heavy formats never require a temp file.
func streamVideo(e entry) (string, func(), error) {
	f, err := os.Open(e.path)
	if err != nil {
		return "", nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return "", nil, err
	}
	h := header{name: e.name, size: info.Size()}
	if e.locked {
		h, err = readHeader(f)
		if err != nil {
			f.Close()
			return "", nil, err
		}
		if info.Size() != h.offset+h.size {
			f.Close()
			return "", nil, errors.New("invalid vault size")
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		f.Close()
		return "", nil, err
	}
	var token [24]byte
	if _, err = rand.Read(token[:]); err != nil {
		listener.Close()
		f.Close()
		return "", nil, err
	}
	path := "/" + hex.EncodeToString(token[:]) + filepath.Ext(h.name)
	handler := contentHandler(f, h, path, e.locked)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	url := "http://" + listener.Addr().String() + path
	return url, func() { server.Close(); f.Close() }, nil
}

func videoHandler(f *os.File, h header, path string) http.Handler {
	return contentHandler(f, h, path, true)
}

func contentHandler(f *os.File, h header, path string, locked bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != path || req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", videoType(filepath.Ext(h.name)))
		var reader io.ReadSeeker = &videoReader{f: f, h: h}
		if !locked {
			reader = io.NewSectionReader(f, 0, h.size)
		}
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
