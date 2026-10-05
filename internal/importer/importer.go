package importer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Dylan-Liew/closeview/internal/parser/generic"
	"github.com/Dylan-Liew/closeview/internal/parser/opencode"
	"github.com/Dylan-Liew/closeview/internal/store"
)

type Options struct {
	Path        string
	Source      string
	Title       string
	ProjectPath string
	Force       bool
}

type Result struct {
	SessionCount int      `json:"sessionCount"`
	SkippedCount int      `json:"skippedCount"`
	Warnings     []string `json:"warnings"`
}

func ImportPath(ctx context.Context, db *store.DB, opts Options) (Result, error) {
	if opts.Path == "" {
		return Result{}, fmt.Errorf("import path is required")
	}
	info, err := os.Stat(opts.Path)
	if err != nil {
		return Result{}, err
	}

	if info.IsDir() {
		opencodeDB := filepath.Join(opts.Path, "opencode.db")
		if _, err := os.Stat(opencodeDB); err == nil && shouldUseOpenCode(opts.Source) {
			return importOpenCodeDB(ctx, db, opencodeDB, opts)
		}
		return importDirectory(ctx, db, opts)
	}

	if generic.IsSQLiteFile(opts.Path) && shouldUseOpenCode(opts.Source) {
		return importOpenCodeDB(ctx, db, opts.Path, opts)
	}
	return importFiles(ctx, db, []string{opts.Path}, opts)
}

func importDirectory(ctx context.Context, db *store.DB, opts Options) (Result, error) {
	var files []string
	if err := filepath.WalkDir(opts.Path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if generic.IsSQLiteFile(path) {
			return nil
		}
		if generic.IsSupportedFile(path) {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("no supported log files found in %s", opts.Path)
	}
	return importFiles(ctx, db, files, opts)
}

func importFiles(ctx context.Context, db *store.DB, files []string, opts Options) (Result, error) {
	result := Result{Warnings: []string{}}
	for _, file := range files {
		session, warnings, err := generic.ParseFile(file, generic.Options{Source: opts.Source, Title: opts.Title, ProjectPath: opts.ProjectPath})
		result.Warnings = append(result.Warnings, warnings...)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		if err := persistSession(ctx, db, &session, opts.Force, false, file, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func importOpenCodeDB(ctx context.Context, db *store.DB, path string, opts Options) (Result, error) {
	sessions, warnings, err := opencode.ParseDB(ctx, path, opencode.Options{Title: opts.Title, ProjectPath: opts.ProjectPath})
	result := Result{Warnings: append([]string{}, warnings...)}
	if err != nil {
		return result, err
	}
	for i := range sessions {
		label := sessions[i].RawPath
		if err := persistSession(ctx, db, &sessions[i], opts.Force, true, label, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func persistSession(ctx context.Context, db *store.DB, session *store.NewSession, force bool, replaceExisting bool, label string, result *Result) error {
	exists, err := db.SessionExistsByHash(ctx, session.SourceHash)
	if err != nil {
		return err
	}
	if exists && !force && !replaceExisting {
		result.SkippedCount++
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s: skipped duplicate import", label))
		return nil
	}
	if exists && force {
		session.SourceHash = session.SourceHash + ":force:" + randomSuffix()
	}
	insert := db.InsertSession
	if replaceExisting && !force {
		insert = db.ReplaceSession
	}
	if _, err := insert(ctx, *session); err != nil {
		return err
	}
	result.SessionCount++
	return nil
}

func randomSuffix() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(bytes[:])
}

func shouldUseOpenCode(source string) bool {
	return source == "" || source == "auto" || source == "opencode" || source == "opencode-db"
}
