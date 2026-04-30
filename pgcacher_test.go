package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- wildcardMatch tests ---

func TestWildcardMatch(t *testing.T) {
	assert.True(t, wildcardMatch("xiaorui.cc", "*rui*"))
	assert.True(t, wildcardMatch("xiaorui.cc", "xiaorui?cc"))
	assert.True(t, wildcardMatch("xiaorui.cc", "xiaorui?cc*"))
	assert.True(t, wildcardMatch("xiaorui.cc", "*xiaorui?cc*"))
	assert.True(t, wildcardMatch("github.com/rfyiamcool", "rfy"))

	// exact match
	assert.True(t, wildcardMatch("hello", "hello"))
	// no match
	assert.False(t, wildcardMatch("hello", "world"))
	// wildcard only
	assert.True(t, wildcardMatch("anything", "*"))
	// single char wildcard
	assert.True(t, wildcardMatch("ab", "a?"))
	assert.False(t, wildcardMatch("abc", "a?"))
}

// --- wildcardMatchMulti tests ---

func TestWildcardMatchMulti(t *testing.T) {
	// comma-separated: match second pattern
	assert.True(t, wildcardMatchMulti("rfyiamcool", "*xiaorui*,rfyiamcool"))
	// comma-separated: match first pattern
	assert.True(t, wildcardMatchMulti("foo.log", "*.log,*.txt"))
	// comma-separated: no match
	assert.False(t, wildcardMatchMulti("foo.doc", "*.log,*.txt"))
	// single pattern (no comma)
	assert.True(t, wildcardMatchMulti("hello.txt", "*.txt"))
	assert.False(t, wildcardMatchMulti("hello.txt", "*.go"))
	// empty patterns string
	assert.False(t, wildcardMatchMulti("anything", ""))
	// spaces around comma
	assert.True(t, wildcardMatchMulti("test.go", "*.txt , *.go"))
}

// --- walkDir / walkDirs tests ---

func TestWalkDir_Depth0(t *testing.T) {
	// depth=0 should list immediate files only, not recurse into subdirs
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "sub", "c.txt"), []byte("c"), 0644)

	files := walkDirs([]string{tmpDir}, 0)
	sort.Strings(files)

	assert.Len(t, files, 2)
	assert.Contains(t, files[0], "a.txt")
	assert.Contains(t, files[1], "b.txt")
}

func TestWalkDir_Depth1(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "sub", "b.txt"), []byte("b"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "sub", "deep"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "sub", "deep", "c.txt"), []byte("c"), 0644)

	files := walkDirs([]string{tmpDir}, 1)
	sort.Strings(files)

	// depth=1: should get a.txt and sub/b.txt but NOT sub/deep/c.txt
	assert.Len(t, files, 2)
	assert.Contains(t, files[0], "a.txt")
	assert.Contains(t, files[1], "b.txt")
}

func TestWalkDirs_FileArg(t *testing.T) {
	// a plain file argument should be returned as-is
	tmpDir := t.TempDir()
	fpath := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(fpath, []byte("test"), 0644)

	files := walkDirs([]string{fpath}, 0)
	assert.Equal(t, []string{fpath}, files)
}

func TestWalkDirs_NonExistent(t *testing.T) {
	// non-existent path is returned as-is (the caller will handle the error)
	files := walkDirs([]string{"/nonexistent/path/xyz"}, 0)
	assert.Equal(t, []string{"/nonexistent/path/xyz"}, files)
}

func TestWalkDirs_Empty(t *testing.T) {
	files := walkDirs(nil, 5)
	assert.Nil(t, files)
}

// --- filterFiles tests ---

func TestFilterFiles(t *testing.T) {
	pg := &pgcacher{
		files:  []string{"/a.txt", "/b.log", "/c.txt", "/a.txt"},
		option: &option{},
	}
	pg.filterFiles()

	// should deduplicate
	assert.Len(t, pg.files, 3)
}

func TestFilterFiles_Exclude(t *testing.T) {
	pg := &pgcacher{
		files:  []string{"/a.txt", "/b.log", "/c.txt"},
		option: &option{excludeFiles: "*.log"},
	}
	pg.filterFiles()

	assert.Len(t, pg.files, 2)
	for _, f := range pg.files {
		assert.NotContains(t, f, ".log")
	}
}

func TestFilterFiles_Include(t *testing.T) {
	pg := &pgcacher{
		files:  []string{"/a.txt", "/b.log", "/c.txt"},
		option: &option{includeFiles: "*.txt"},
	}
	pg.filterFiles()

	assert.Len(t, pg.files, 2)
	for _, f := range pg.files {
		assert.Contains(t, f, ".txt")
	}
}

// --- ignoreFile tests ---

func TestIgnoreFile(t *testing.T) {
	// no filters: nothing ignored
	pg := &pgcacher{option: &option{}}
	assert.False(t, pg.ignoreFile("/some/file.txt"))

	// exclude matches
	pg = &pgcacher{option: &option{excludeFiles: "*.log,*.tmp"}}
	assert.True(t, pg.ignoreFile("/some/file.log"))
	assert.True(t, pg.ignoreFile("/some/file.tmp"))
	assert.False(t, pg.ignoreFile("/some/file.txt"))

	// include: only *.go files pass
	pg = &pgcacher{option: &option{includeFiles: "*.go"}}
	assert.False(t, pg.ignoreFile("/main.go"))
	assert.True(t, pg.ignoreFile("/readme.md"))
}
