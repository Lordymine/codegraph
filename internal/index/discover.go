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
// hard-ignores and a simple .cbmignore (one glob per line, repo-relative).
// NOTE: full .gitignore semantics are a later milestone; this is the 80% case.
func Discover(root string) ([]SourceFile, error) {
	root, _ = filepath.Abs(root)
	ignore := loadCbmIgnore(root)

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

type cbmIgnore struct{ globs []string }

func loadCbmIgnore(root string) cbmIgnore {
	f, err := os.Open(filepath.Join(root, ".cbmignore"))
	if err != nil {
		return cbmIgnore{}
	}
	defer f.Close()
	var globs []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		globs = append(globs, strings.TrimSuffix(filepath.ToSlash(line), "/"))
	}
	return cbmIgnore{globs: globs}
}

func (c cbmIgnore) matchDir(rel string) bool  { return c.match(rel) }
func (c cbmIgnore) matchFile(rel string) bool { return c.match(rel) }

func (c cbmIgnore) match(rel string) bool {
	for _, g := range c.globs {
		g = strings.TrimPrefix(g, "**/")
		if rel == g || strings.HasPrefix(rel, g+"/") || strings.HasSuffix(rel, strings.TrimPrefix(g, "/")) {
			return true
		}
		if ok, _ := filepath.Match(g, filepath.Base(rel)); ok {
			return true
		}
	}
	return false
}
