package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const magic = "VAULTV1\n"
const headerSize = 8 + 16 + 8 + 2 + 32
const extension = ".vlt"

// This is deliberately a format constant, not a secret. See README for recovery.
var formatKey = sha256.Sum256([]byte("vault-v1-reversible-video-obfuscation"))

var videoExt = map[string]bool{
	".mp4": true, ".mkv": true, ".mov": true, ".m4v": true,
	".avi": true, ".webm": true, ".wmv": true, ".flv": true,
	".mpg": true, ".mpeg": true, ".ts": true, ".3gp": true,
}

type entry struct {
	path   string
	name   string
	size   int64
	locked bool
}

type header struct {
	iv     [16]byte
	size   int64
	name   string
	digest [32]byte
	offset int64
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "v:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command, root := "browse", "."
	if len(args) > 0 {
		switch args[0] {
		case "lock", "unlock":
			command = args[0]
			args = args[1:]
		case "help", "-h", "--help":
			fmt.Println("v [directory]       Browse and play\nv lock [directory]  Lock videos\nv unlock [directory]  Restore videos")
			return nil
		}
	}
	if len(args) > 1 {
		return errors.New("usage: v [directory] | v lock [directory] | v unlock [directory]")
	}
	if len(args) == 1 {
		root = args[0]
	}
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	items, err := scan(root)
	if err != nil {
		return err
	}
	switch command {
	case "lock":
		processErr := process(items, false)
		return errors.Join(processErr, maintainThumbnailCache(root, true))
	case "unlock":
		processErr := process(items, true)
		return errors.Join(processErr, maintainThumbnailCache(root, false))
	default:
		if err := maintainThumbnailCache(root, false); err != nil {
			return err
		}
		return browse(root, items)
	}
}

func scan(root string) ([]entry, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", root)
	}
	var items []entry
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink unsupported: %s", path)
		}
		if d.IsDir() {
			if d.Name() == thumbnailCacheDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("not a regular file: %s", path)
		}
		if d.Name() == ".DS_Store" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if strings.HasSuffix(d.Name(), extension) {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			h, readErr := readHeader(f)
			closeErr := f.Close()
			if readErr != nil {
				return fmt.Errorf("%s: %w", path, readErr)
			}
			if closeErr != nil {
				return closeErr
			}
			if info.Size() != h.offset+h.size {
				return fmt.Errorf("invalid size: %s", path)
			}
			items = append(items, entry{path, h.name, h.size, true})
			return nil
		}
		if !videoExt[strings.ToLower(filepath.Ext(d.Name()))] {
			return fmt.Errorf("non-video file: %s", path)
		}
		items = append(items, entry{path, d.Name(), info.Size(), false})
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	return items, err
}

func nameMask(iv [16]byte, n int) []byte {
	block, _ := aes.NewCipher(formatKey[:])
	var nameIV [16]byte
	copy(nameIV[:], iv[:])
	nameIV[0] ^= 0x80
	mask := make([]byte, n)
	cipher.NewCTR(block, nameIV[:]).XORKeyStream(mask, mask)
	return mask
}

func readHeader(r io.Reader) (header, error) {
	var h header
	buf := make([]byte, headerSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return h, errors.New("invalid vault header")
	}
	if string(buf[:8]) != magic {
		return h, errors.New("invalid vault format")
	}
	copy(h.iv[:], buf[8:24])
	u := binary.BigEndian.Uint64(buf[24:32])
	if u > 1<<63-1 {
		return h, errors.New("invalid video size")
	}
	h.size = int64(u)
	nameLen := int(binary.BigEndian.Uint16(buf[32:34]))
	if nameLen == 0 || nameLen > 4096 {
		return h, errors.New("invalid video name")
	}
	copy(h.digest[:], buf[34:66])
	name := make([]byte, nameLen)
	if _, err := io.ReadFull(r, name); err != nil {
		return h, errors.New("invalid vault name")
	}
	mask := nameMask(h.iv, nameLen)
	for i := range name {
		name[i] ^= mask[i]
	}
	h.name = string(name)
	if filepath.Base(h.name) != h.name || !videoExt[strings.ToLower(filepath.Ext(h.name))] {
		return h, errors.New("invalid video name")
	}
	h.offset = headerSize + int64(nameLen)
	return h, nil
}

func writeHeader(w io.Writer, h header) error {
	name := []byte(h.name)
	if len(name) == 0 || len(name) > 4096 {
		return errors.New("video name too long")
	}
	buf := make([]byte, headerSize+len(name))
	copy(buf[:8], magic)
	copy(buf[8:24], h.iv[:])
	binary.BigEndian.PutUint64(buf[24:32], uint64(h.size))
	binary.BigEndian.PutUint16(buf[32:34], uint16(len(name)))
	copy(buf[34:66], h.digest[:])
	mask := nameMask(h.iv, len(name))
	for i, b := range name {
		buf[headerSize+i] = b ^ mask[i]
	}
	_, err := w.Write(buf)
	return err
}

func process(items []entry, unlock bool) error {
	var selected []entry
	var total int64
	for _, e := range items {
		if e.locked == unlock {
			selected = append(selected, e)
			total += e.size
		}
	}
	if len(selected) == 0 {
		fmt.Println("No videos to process.")
		return nil
	}
	p := &progress{verb: "Locking", total: total, start: time.Now()}
	if unlock {
		p.verb = "Unlocking"
	}
	for _, e := range selected {
		var err error
		if unlock {
			err = unlockOne(e, p)
		} else {
			err = lockOne(e, p)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr)
			return fmt.Errorf("%s: %w", e.path, err)
		}
	}
	p.finish(len(selected))
	return nil
}

type progress struct {
	verb        string
	total, done int64
	start, last time.Time
}

func (p *progress) add(n int) {
	p.done += int64(n)
	if time.Since(p.last) < 100*time.Millisecond && p.done != p.total {
		return
	}
	p.last = time.Now()
	percent := 100.0
	if p.total > 0 {
		percent = 100 * float64(p.done) / float64(p.total)
	}
	fmt.Fprintf(os.Stderr, "\r%s %.0f%%  %s/%s", p.verb, percent, human(p.done), human(p.total))
}
func (p *progress) finish(n int) { p.add(0); fmt.Fprintf(os.Stderr, "\n%d videos.\n", n) }
func human(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	if n < 1<<30 {
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
}

func tempFile(dir string) (*os.File, error) { return os.CreateTemp(dir, ".vault-tmp-*") }
func durableClose(f *os.File) error {
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func publish(temp, final string) error {
	if err := os.Link(temp, final); err != nil {
		return err
	}
	return os.Remove(temp)
}

func lockOne(e entry, p *progress) error {
	in, err := os.Open(e.path)
	if err != nil {
		return err
	}
	defer in.Close()
	stat, err := in.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() != e.size {
		return errors.New("file changed during scan")
	}
	var iv [16]byte
	if _, err = rand.Read(iv[:]); err != nil {
		return err
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	final := filepath.Join(filepath.Dir(e.path), ".vault-"+hex.EncodeToString(id[:])+extension)
	if _, err = os.Lstat(final); err == nil {
		return errors.New("vault file already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, err := tempFile(filepath.Dir(e.path))
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	h := header{iv: iv, size: e.size, name: e.name}
	if err = writeHeader(out, h); err != nil {
		out.Close()
		return err
	}
	block, _ := aes.NewCipher(formatKey[:])
	stream := cipher.NewCTR(block, iv[:])
	hash := sha256.New()
	buf := make([]byte, 1<<20)
	var copied int64
	for copied < e.size {
		limit := int64(len(buf))
		if e.size-copied < limit {
			limit = e.size - copied
		}
		n, readErr := in.Read(buf[:limit])
		if n > 0 {
			hash.Write(buf[:n])
			stream.XORKeyStream(buf[:n], buf[:n])
			if _, err = out.Write(buf[:n]); err != nil {
				out.Close()
				return err
			}
			p.add(n)
			copied += int64(n)
		}
		if readErr == io.EOF {
			out.Close()
			return errors.New("source shortened during lock")
		}
		if readErr != nil {
			out.Close()
			return readErr
		}
	}
	if _, err = out.Seek(34, io.SeekStart); err != nil {
		out.Close()
		return err
	}
	if _, err = out.Write(hash.Sum(nil)); err != nil {
		out.Close()
		return err
	}
	if err = durableClose(out); err != nil {
		return err
	}
	if err = publish(out.Name(), final); err != nil {
		return err
	}
	if err = os.Remove(e.path); err != nil {
		return fmt.Errorf("locked copy at %s; original remains: %w", final, err)
	}
	return nil
}

func unlockOne(e entry, p *progress) error {
	cachePath, cacheErr := thumbnailCachePath(e)
	if cacheErr != nil {
		return cacheErr
	}
	in, err := os.Open(e.path)
	if err != nil {
		return err
	}
	defer in.Close()
	h, err := readHeader(in)
	if err != nil {
		return err
	}
	stat, err := in.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != h.offset+h.size {
		return errors.New("file changed during scan")
	}
	final := filepath.Join(filepath.Dir(e.path), h.name)
	if _, err = os.Lstat(final); err == nil {
		return fmt.Errorf("destination exists: %s", final)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, err := tempFile(filepath.Dir(e.path))
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	block, _ := aes.NewCipher(formatKey[:])
	stream := cipher.NewCTR(block, h.iv[:])
	hash := sha256.New()
	buf := make([]byte, 1<<20)
	var copied int64
	for copied < h.size {
		limit := int64(len(buf))
		if h.size-copied < limit {
			limit = h.size - copied
		}
		n, readErr := io.ReadFull(in, buf[:limit])
		if readErr != nil {
			out.Close()
			return readErr
		}
		stream.XORKeyStream(buf[:n], buf[:n])
		hash.Write(buf[:n])
		if _, err = out.Write(buf[:n]); err != nil {
			out.Close()
			return err
		}
		copied += int64(n)
		p.add(n)
	}
	if sum := hash.Sum(nil); !equal(sum, h.digest[:]) {
		out.Close()
		return errors.New("checksum mismatch; encrypted file kept")
	}
	if err = durableClose(out); err != nil {
		return err
	}
	if err = publish(out.Name(), final); err != nil {
		return err
	}
	if err = os.Remove(e.path); err != nil {
		return fmt.Errorf("restored copy at %s; encrypted file remains: %w", final, err)
	}
	if err := os.Remove(cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("video restored; remove thumbnail cache: %w", err)
	}
	// A nonempty directory belongs to other locked videos. The command's
	// reconciliation handles orphaned entries once processing finishes.
	_ = os.Remove(filepath.Dir(cachePath))
	return nil
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
