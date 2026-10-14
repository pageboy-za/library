package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const statusTTL = 30 * time.Second
const statusMaxEntries = 1000
const statusMaxFiles = 2000
const statusMaxManifest = 64 << 10
const statusMaxFileBytes = 32 << 20
const statusMaxTotalBytes = 128 << 20

var errHealthBudget = errors.New("dashboard verification budget exceeded")
var errHealthUnsafe = errors.New("unsafe backup paths")
var errHealthTimestamp = errors.New("invalid backup creation timestamp")

type Status struct {
	Configured     bool   `json:"configured"`
	State          string `json:"state"`
	LastVerifiedAt string `json:"last_verified_at,omitempty"`
	LastAttemptAt  string `json:"last_attempt_at,omitempty"`
	BackupCount    int    `json:"backup_count"`
	Message        string `json:"message"`
	Stale          bool   `json:"stale"`
	CheckedAt      string `json:"checked_at"`
}

// HealthService only reads snapshots. Its small verification window and byte
// limits keep kiosk requests practical; CLI Verify remains the full-size check.
type HealthService struct {
	directory string
	mu        sync.Mutex
	cached    Status
	cachedAt  time.Time
	now       func() time.Time
}

func NewHealthService(directory string) *HealthService {
	return &HealthService{directory: directory, now: time.Now}
}
func (h *HealthService) Status(ctx context.Context) Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now().UTC()
	if !h.cachedAt.IsZero() && now.Sub(h.cachedAt) < statusTTL && now.Sub(h.cachedAt) >= 0 {
		return h.cached
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out := h.inspect(ctx, now)
	h.cached = out
	h.cachedAt = now
	return out
}

type statusCandidate struct {
	path     string
	stamp    time.Time
	manifest Manifest
	err      error
}

func (h *HealthService) inspect(ctx context.Context, now time.Time) Status {
	out := Status{Configured: h.directory != "", State: "unconfigured", CheckedAt: now.Format(time.RFC3339), Message: "Backup monitoring is not configured."}
	if !out.Configured {
		return out
	}
	out.State = "no_backups"
	out.Message = "No backup folders found yet."
	root, e := safeStatusRoot(h.directory)
	if os.IsNotExist(e) {
		out.State = "failed"
		out.Message = "The configured backup folder is unavailable."
		return out
	}
	if e != nil {
		out.State = "failed"
		out.Message = "The backup folder could not be checked safely."
		return out
	}
	dir, e := os.Open(root)
	if e != nil {
		out.State = "failed"
		out.Message = "The backup folder is unavailable."
		return out
	}
	entries, e := dir.ReadDir(statusMaxEntries + 1)
	dir.Close()
	if e != nil && e != io.EOF {
		out.State = "failed"
		out.Message = "The backup folder could not be read."
		return out
	}
	candidates := []statusCandidate{}
	for i, entry := range entries {
		if i >= statusMaxEntries {
			out.State = "failed"
			out.Message = "Too many backup entries for the dashboard to check. Use the backup verification command."
			return out
		}
		if ctx.Err() != nil {
			out.State = "failed"
			out.Message = "Backup verification timed out. Use the backup verification command."
			return out
		}
		info, e := entry.Info()
		if e != nil {
			out.State = "failed"
			out.Message = "A backup entry could not be read safely."
			return out
		}
		if info.Mode()&os.ModeSymlink != 0 {
			candidates = append(candidates, statusCandidate{path: filepath.Join(root, entry.Name()), stamp: info.ModTime().UTC(), err: errHealthUnsafe})
			continue
		}
		if !info.IsDir() {
			continue
		}
		if entry.Name() == ".library-backup.lock" {
			continue
		}
		out.BackupCount++
		c := statusCandidate{path: filepath.Join(root, entry.Name()), stamp: info.ModTime().UTC()}
		c.manifest, c.err = readStatusManifest(c.path, now)
		if c.err == nil {
			c.stamp, _ = time.Parse(time.RFC3339, c.manifest.CreatedAt)
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return out
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].stamp.Equal(candidates[j].stamp) {
			return candidates[i].path > candidates[j].path
		}
		return candidates[i].stamp.After(candidates[j].stamp)
	})
	latest := candidates[0]
	out.LastAttemptAt = latest.stamp.UTC().Format(time.RFC3339)
	out.State = "failed"
	out.Message = "The latest backup could not be verified. Use the backup verification command."
	for i, c := range candidates {
		if i >= 3 || ctx.Err() != nil {
			break
		}
		err := c.err
		if err == nil {
			err = verifyStatusCandidate(ctx, c.path, c.manifest)
		}
		if i == 0 && err != nil {
			switch {
			case errors.Is(err, errHealthBudget):
				out.Message = "The latest backup exceeds dashboard verification limits. Use the backup verification command."
			case errors.Is(err, errHealthUnsafe):
				out.Message = "The latest backup contains unsafe paths and could not be verified."
			case errors.Is(err, errHealthTimestamp):
				out.Message = "The latest backup has an invalid creation date and could not be verified."
			case ctx.Err() != nil:
				out.Message = "Backup verification timed out. Use the backup verification command."
			}
		}
		if err == nil {
			out.LastVerifiedAt = c.manifest.CreatedAt
			created, _ := time.Parse(time.RFC3339, c.manifest.CreatedAt)
			out.Stale = now.Sub(created) > 48*time.Hour
			if i == 0 {
				out.State = "verified"
				out.Message = "The latest backup passed verification."
				if out.Stale {
					out.Message = "The latest verified backup is more than two days old."
				}
			}
			break
		}
	}
	return out
}

// Reject links in the configured root and its ancestors before opening it.
func safeStatusRoot(path string) (string, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errHealthUnsafe
		}
	}
	return abs, nil
}
func readStatusManifest(dir string, now time.Time) (Manifest, error) {
	var m Manifest
	path := filepath.Join(dir, "manifest.json")
	info, e := os.Lstat(path)
	if e != nil {
		return m, e
	}
	if !info.Mode().IsRegular() {
		return m, errHealthUnsafe
	}
	if info.Size() > statusMaxManifest {
		return m, errHealthBudget
	}
	f, e := os.Open(path)
	if e != nil {
		return m, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return m, errHealthUnsafe
	}
	raw, e := io.ReadAll(io.LimitReader(f, statusMaxManifest+1))
	if e != nil {
		return m, e
	}
	if len(raw) > statusMaxManifest {
		return m, errHealthBudget
	}
	if e = json.Unmarshal(raw, &m); e != nil {
		return m, e
	}
	if m.Format != 1 {
		return m, errors.New("unsupported backup format")
	}
	created, e := time.Parse(time.RFC3339, m.CreatedAt)
	if e != nil || created.After(now.Add(5*time.Minute)) {
		return m, errHealthTimestamp
	}
	m.CreatedAt = created.UTC().Format(time.RFC3339)
	return m, nil
}
func verifyStatusCandidate(ctx context.Context, root string, m Manifest) error {
	if len(m.Files) > statusMaxFiles {
		return errHealthBudget
	}
	seen := map[string]File{}
	var total int64
	for _, file := range m.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !safePath(file.Path) {
			return errHealthUnsafe
		}
		if _, ok := seen[file.Path]; ok {
			return errHealthUnsafe
		}
		if e := noSymlinks(root, file.Path); e != nil {
			return errHealthUnsafe
		}
		info, e := os.Lstat(filepath.Join(root, filepath.FromSlash(file.Path)))
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errHealthUnsafe
		}
		if file.Bytes < 0 || info.Size() != file.Bytes {
			return errors.New("backup length mismatch")
		}
		if file.Bytes > statusMaxFileBytes {
			return errHealthBudget
		}
		total += file.Bytes
		if total > statusMaxTotalBytes {
			return errHealthBudget
		}
		seen[file.Path] = file
	}
	if _, ok := seen["library.sqlite"]; !ok {
		return errors.New("missing database")
	}
	if _, ok := seen["catalogue.json"]; !ok {
		return errors.New("missing export")
	}
	for path, want := range seen {
		actual, e := statusDigest(ctx, filepath.Join(root, filepath.FromSlash(path)))
		if e != nil {
			return e
		}
		if actual.SHA256 != want.SHA256 || actual.Bytes != want.Bytes {
			return errors.New("backup checksum mismatch")
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	db, e := openRead(filepath.Join(root, "library.sqlite"), true)
	if e != nil {
		return e
	}
	defer db.Close()
	var check string
	if e = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); e != nil {
		return e
	}
	if check != "ok" {
		return errors.New("database integrity failed")
	}
	if e = verifySnapshotForeignKeys(ctx, db); e != nil {
		return e
	}
	var version int
	if e = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil {
		return e
	}
	if version != m.SchemaVersion {
		return errors.New("schema version mismatch")
	}
	covers, e := coverFiles(ctx, db)
	if e != nil {
		return e
	}
	for path, sum := range covers {
		file, ok := seen["covers/"+path]
		if !ok || file.SHA256 != sum {
			return errors.New("missing or mismatched cover")
		}
	}
	return nil
}
func statusDigest(ctx context.Context, path string) (File, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return File{}, e
	}
	if !info.Mode().IsRegular() {
		return File{}, errHealthUnsafe
	}
	f, e := os.Open(path)
	if e != nil {
		return File{}, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return File{}, errHealthUnsafe
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var count int64
	for {
		if e = ctx.Err(); e != nil {
			return File{}, e
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			count += int64(n)
			if count > statusMaxFileBytes {
				return File{}, errHealthBudget
			}
			hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return File{}, readErr
		}
	}
	return File{SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: count}, nil
}
