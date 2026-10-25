// Package backup keeps dated copies of the database, prunes them, mirrors them to an external
// folder and detects a database that suddenly lost its content.
package backup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Source is the database being protected.
type Source interface {
	BackupTo(dst string) error
	Counts() (customers, quotes, invoices int, err error)
	Signature() (string, error)
}

// Info describes one backup file.
type Info struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Time    time.Time `json:"time"`
	Reason  string    `json:"reason,omitempty"`
	Monthly bool      `json:"monthly"`
}

// State is persisted in backups/state.json.
type State struct {
	LastSignature string    `json:"last_signature"`
	LastBackup    time.Time `json:"last_backup"`
	LastExternal  time.Time `json:"last_external"`
	MaxDocs       int       `json:"max_docs"`
	LastDocs      int       `json:"last_docs"`
}

// Status is shown in the UI.
type Status struct {
	State
	Dir          string `json:"dir"`
	ExternalDir  string `json:"external_dir"`
	ExternalOK   bool   `json:"external_ok"`
	Count        int    `json:"count"`
	Alert        string `json:"alert"`
	ExternalLate bool   `json:"external_late"`
}

// Manager performs backups.
type Manager struct {
	mu       sync.Mutex
	Dir      string
	Keep     func() int
	External func() string
	Log      func(format string, args ...any)
	state    State
	alert    string
}

// New creates a manager storing backups in dir.
func New(dir string, keep func() int, external func() string, log func(string, ...any)) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	m := &Manager{Dir: dir, Keep: keep, External: external, Log: log}
	if b, err := os.ReadFile(filepath.Join(dir, "state.json")); err == nil {
		json.Unmarshal(b, &m.state)
	}
	return m, nil
}

func (m *Manager) saveState() {
	b, _ := json.MarshalIndent(m.state, "", "  ")
	tmp := filepath.Join(m.Dir, "state.json.tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(m.Dir, "state.json"))
	}
}

// Check compares the current content with the largest content ever seen and sets an alert
// when more than half of the documents disappeared (the failure that hit the old ArtiMan).
func (m *Manager) Check(src Source) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, q, i, err := src.Counts()
	if err != nil {
		m.alert = "Impossible de lire la base : " + err.Error()
		return m.alert
	}
	docs := c + q + i
	m.alert = ""
	if m.state.MaxDocs > 20 && docs < m.state.MaxDocs/2 {
		m.alert = fmt.Sprintf("La base contient %d éléments alors qu'elle en a compté jusqu'à %d. Elle a peut-être été endommagée : "+
			"restaurez une sauvegarde depuis l'écran Sauvegardes.", docs, m.state.MaxDocs)
	}
	return m.alert
}

// Run makes a backup if the content changed since the last one (or always when force is true).
func (m *Manager) Run(src Source, reason string, force bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sig, err := src.Signature()
	if err != nil {
		return "", err
	}
	if !force && sig == m.state.LastSignature {
		return "", nil
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil { // the folder may have been deleted by hand
		return "", err
	}
	stamp := time.Now().Format("2006-01-02_150405")
	name := fmt.Sprintf("artiman_%s_%s.db", stamp, reason)
	for i := 2; fileExists(filepath.Join(m.Dir, name)); i++ {
		name = fmt.Sprintf("artiman_%s_%s-%d.db", stamp, reason, i)
	}
	dst := filepath.Join(m.Dir, name)
	if err := src.BackupTo(dst); err != nil {
		return "", fmt.Errorf("sauvegarde impossible : %w", err)
	}
	c, q, i, _ := src.Counts()
	docs := c + q + i
	m.state.LastSignature = sig
	m.state.LastBackup = time.Now()
	m.state.LastDocs = docs
	// A sudden collapse must not become the new reference.
	if docs > m.state.MaxDocs && m.alert == "" {
		m.state.MaxDocs = docs
	}
	m.saveState()
	m.Log("backup %s (%s)", name, reason)
	m.prune()
	if ext := strings.TrimSpace(m.External()); ext != "" {
		if err := m.copyExternal(ext, dst); err != nil {
			m.Log("external backup failed: %v", err)
		}
	}
	return name, nil
}

// List returns backups, newest first.
func (m *Manager) List() []Info {
	entries, _ := os.ReadDir(m.Dir)
	var out []Info
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "artiman_") || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		info := Info{Name: e.Name(), Size: fi.Size(), Time: fi.ModTime()}
		if parts := strings.SplitN(strings.TrimSuffix(e.Name(), ".db"), "_", 4); len(parts) == 4 {
			info.Reason = parts[3]
			if t, err := time.ParseInLocation("2006-01-02_150405", parts[1]+"_"+parts[2], time.Local); err == nil {
				info.Time = t
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// prune keeps the newest Keep() backups, the first backup of every month and the largest backup.
func (m *Manager) prune() {
	all := m.List()
	keep := m.Keep()
	if keep < 5 {
		keep = 5
	}
	protected := map[string]bool{}
	var largest Info
	months := map[string]Info{}
	for _, b := range all {
		if b.Size > largest.Size {
			largest = b
		}
		k := b.Time.Format("2006-01")
		if cur, ok := months[k]; !ok || b.Time.Before(cur.Time) {
			months[k] = b
		}
	}
	protected[largest.Name] = true
	for _, b := range months {
		protected[b.Name] = true
	}
	for i, b := range all {
		if i < keep || protected[b.Name] {
			continue
		}
		if err := os.Remove(filepath.Join(m.Dir, b.Name)); err == nil {
			m.Log("backup pruned %s", b.Name)
		}
	}
}

func (m *Manager) copyExternal(dir, src string) error {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("dossier externe indisponible : %s", dir)
	}
	target := filepath.Join(dir, "ArtiMan-sauvegardes")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := copyFile(src, filepath.Join(target, filepath.Base(src))); err != nil {
		return err
	}
	m.state.LastExternal = time.Now()
	m.saveState()
	// Keep the 60 newest copies externally.
	entries, _ := os.ReadDir(target)
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "artiman_") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for i, n := range names {
		if i >= 60 {
			os.Remove(filepath.Join(target, n))
		}
	}
	m.Log("external copy to %s", target)
	return nil
}

// CopyExternalNow mirrors the latest backup to the external folder.
func (m *Manager) CopyExternalNow() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ext := strings.TrimSpace(m.External())
	if ext == "" {
		return fmt.Errorf("aucun dossier externe n'est configuré")
	}
	all := m.List()
	if len(all) == 0 {
		return fmt.Errorf("aucune sauvegarde à copier")
	}
	return m.copyExternal(ext, filepath.Join(m.Dir, all[0].Name))
}

// Status reports the backup situation for the UI.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{State: m.state, Dir: m.Dir, ExternalDir: m.External(), Alert: m.alert, Count: len(m.List())}
	if st.ExternalDir != "" {
		if fi, err := os.Stat(st.ExternalDir); err == nil && fi.IsDir() {
			st.ExternalOK = true
		}
	}
	st.ExternalLate = m.state.LastExternal.IsZero() || time.Since(m.state.LastExternal) > 7*24*time.Hour
	return st
}

// Path returns the full path of a backup after validating its name.
func (m *Manager) Path(name string) (string, error) {
	if name != filepath.Base(name) || !strings.HasPrefix(name, "artiman_") || !strings.HasSuffix(name, ".db") {
		return "", fmt.Errorf("nom de sauvegarde invalide")
	}
	p := filepath.Join(m.Dir, name)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("sauvegarde introuvable")
	}
	return p, nil
}

// ResetReference forgets the "largest content" reference (after a deliberate restore).
func (m *Manager) ResetReference(docs int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.MaxDocs = docs
	m.state.LastSignature = ""
	m.alert = ""
	m.saveState()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	out.Close()
	return os.Rename(tmp, dst)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// CopyFile is exported for restores.
func CopyFile(src, dst string) error { return copyFile(src, dst) }
