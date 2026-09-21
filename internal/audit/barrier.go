package audit

import (
	"os"
	"path/filepath"
	"strings"
)

// FileEntry represents a discovered file and its classification in the barrier.
type FileEntry struct {
	RelPath  string `json:"rel_path"`
	FullPath string `json:"full_path"`
	Size     int64  `json:"size"`
	IsDoc    bool   `json:"is_doc"`
	IsTest   bool   `json:"is_test"`
	IsSource bool   `json:"is_source"`
}

// AuditContext holds the partitioned views across the information barrier.
type AuditContext struct {
	Root           string      `json:"root"`
	SpecFiles      []FileEntry `json:"spec_files"`      // Full view: source + docs
	BlindfoldFiles []FileEntry `json:"blindfold_files"` // Isolated view: strictly source/tests, zero docs
	DocFiles       []FileEntry `json:"doc_files"`       // Only docs
}

// IsDocFile checks whether a path represents documentation or specification artifacts.
func IsDocFile(relPath string) bool {
	clean := filepath.ToSlash(relPath)
	lower := strings.ToLower(clean)

	// Top-level or nested documentation directories
	parts := strings.Split(lower, "/")
	for _, p := range parts {
		if p == "docs" || p == "doc" || p == ".github" || p == "documentation" {
			return true
		}
	}

	// Extensions
	ext := strings.ToLower(filepath.Ext(clean))
	switch ext {
	case ".md", ".markdown", ".rst", ".adoc", ".tex":
		return true
	case ".txt":
		// Plain text documentation like README.txt or LICENSE
		base := strings.ToLower(filepath.Base(clean))
		if strings.HasPrefix(base, "readme") || strings.HasPrefix(base, "license") || strings.HasPrefix(base, "todo") {
			return true
		}
	}

	base := strings.ToLower(filepath.Base(clean))
	if base == "license" || base == "notice" || base == "contributing" {
		return true
	}

	return false
}

// IsSourceFile checks whether a path represents compilable or interpretable source code.
func IsSourceFile(relPath string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	switch ext {
	case ".go", ".py", ".ts", ".js", ".tsx", ".jsx", ".c", ".cpp", ".cc", ".h", ".hpp",
		".rs", ".java", ".scala", ".kt", ".rb", ".php", ".sh", ".bash", ".zsh",
		".sql", ".lua", ".swift", ".m", ".proto":
		return true
	default:
		return false
	}
}

// IsTestFile checks whether a file is a test or fixture.
func IsTestFile(relPath string) bool {
	lower := strings.ToLower(filepath.ToSlash(relPath))
	if strings.Contains(lower, "/tests/") || strings.Contains(lower, "/test/") || strings.Contains(lower, "/fixtures/") {
		return true
	}
	base := strings.ToLower(filepath.Base(lower))
	if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_test.py") ||
		strings.HasPrefix(base, "test_") || strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".spec.ts") || strings.HasSuffix(base, ".test.js") {
		return true
	}
	return false
}

// BuildBarrier scans the target directory and partitions files into Spec and Blindfold views.
func BuildBarrier(root string) (*AuditContext, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	ctx := &AuditContext{
		Root:           absRoot,
		SpecFiles:      make([]FileEntry, 0),
		BlindfoldFiles: make([]FileEntry, 0),
		DocFiles:       make([]FileEntry, 0),
	}

	err = filepath.Walk(absRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".venv" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}

		entry := FileEntry{
			RelPath:  rel,
			FullPath: path,
			Size:     info.Size(),
			IsDoc:    IsDocFile(rel),
			IsTest:   IsTestFile(rel),
			IsSource: IsSourceFile(rel),
		}

		ctx.SpecFiles = append(ctx.SpecFiles, entry)

		if entry.IsDoc {
			ctx.DocFiles = append(ctx.DocFiles, entry)
		} else if entry.IsSource || entry.IsTest {
			// Hard Information Barrier: strictly source code and tests, zero docs
			ctx.BlindfoldFiles = append(ctx.BlindfoldFiles, entry)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return ctx, nil
}
