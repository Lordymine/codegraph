package index

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Lang is a detected source language for a file.
type Lang string

const (
	LangGo  Lang = "go"
	LangTS  Lang = "ts"
	LangTSX Lang = "tsx"
	LangJS  Lang = "js"
)

// SourceFile is a discovered file worth indexing.
type SourceFile struct {
	AbsPath string
	RelPath string
	Lang    Lang
}

var langByExt = map[string]Lang{
	".go":  LangGo,
	".ts":  LangTS,
	".tsx": LangTSX,
	".js":  LangJS,
	".jsx": LangJS,
	".mjs": LangJS,
	".cjs": LangJS,
}

// hardcoded ignores, same spirit as upstream (.git, node_modules, build dirs).
var hardIgnoreDir = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	".next": true, ".expo": true, "coverage": true, "vendor": true,
	".cache": true, "_upstream": true, "android": true, "ios": true,
}

// Discover walks root and returns indexable source files. It honors directory
// hard-ignores plus the repo's .gitignore and .cbmignore — so a repo's vendored
// deps and build artifacts (e.g. a Go module cache under tmp/) don't flood the
// graph. Common-case ignore semantics only: directory/name patterns, globs, and
// root-anchored paths; negation (`!`) and nested .gitignore files are not honored.
func Discover(root string) ([]SourceFile, error) {
	root, _ = filepath.Abs(root)
	ignore := loadIgnore(root)

	var files []SourceFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if hardIgnoreDir[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			if ignore.matchDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		lang, ok := langByExt[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil
		}
		if ignore.matchFile(rel) {
			return nil
		}
		files = append(files, SourceFile{AbsPath: path, RelPath: rel, Lang: lang})
		return nil
	})
	return files, err
}

type ignoreSet struct{ patterns []string }

// loadIgnore reads the repo's .gitignore and .cbmignore into one matcher.
func loadIgnore(root string) ignoreSet {
	var pats []string
	pats = append(pats, readIgnoreFile(filepath.Join(root, ".gitignore"))...)
	pats = append(pats, readIgnoreFile(filepath.Join(root, ".cbmignore"))...)
	return ignoreSet{patterns: pats}
}

func readIgnoreFile(file string) []string {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue // blank, comment, or unsupported negation
		}
		out = append(out, filepath.ToSlash(line))
	}
	return out
}

func (ig ignoreSet) matchDir(rel string) bool  { return ig.match(rel) }
func (ig ignoreSet) matchFile(rel string) bool { return ig.match(rel) }

// match applies common-case .gitignore semantics: a pattern with no slash matches
// that basename at any depth (file or dir); a pattern with a slash is anchored to
// the repo root (exact, directory-prefix, or glob over the full relative path).
func (ig ignoreSet) match(rel string) bool {
	base := rel
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		base = rel[i+1:]
	}
	for _, p := range ig.patterns {
		p = strings.TrimPrefix(p, "**/")
		trimmed := strings.TrimSuffix(p, "/")
		if trimmed == "" {
			continue
		}
		name := strings.TrimPrefix(trimmed, "/")
		anchored := strings.HasPrefix(trimmed, "/") || strings.Contains(name, "/")
		if anchored {
			if rel == name || strings.HasPrefix(rel, name+"/") {
				return true
			}
			if ok, _ := filepath.Match(name, rel); ok {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(name, base); ok {
			return true
		}
	}
	return false
}
