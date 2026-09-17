package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"codnect.io/chrono"
)

func cleanDatas() error {
	pasteFolder := getPasteFolder()

	if _, err := os.Stat(pasteFolder); err != nil {
		return fmt.Errorf("fail to open paste folder %s: %w", pasteFolder, err)
	}

	// Collect first, check afterwards: ReadPaste deletes expired pastes and
	// prunes their parent directories, mutating the tree the walk is still
	// traversing.
	var pasteIds []string

	err := filepath.WalkDir(pasteFolder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A single unreadable entry must not abort the whole pass.
			slog.Warn("[SCHEDULER] skip entry", "path", path, "err", err)
			return nil
		}

		if entry.IsDir() || !isValidUUID(entry.Name()) {
			return nil
		}

		pasteIds = append(pasteIds, entry.Name())

		return nil
	})

	if err != nil {
		return fmt.Errorf("fail to walk paste folder %s: %w", pasteFolder, err)
	}

	for _, pasteId := range pasteIds {
		// Debug and not info: one line per stored paste on every pass drowned
		// the journal, and the identifier is half of a paste url.
		slog.Debug("[SCHEDULER] check paste", "paste_id", pasteId)

		// Still valid pastes are left untouched: the read counter does not move
		// for an invalid user-agent, so nothing is rewritten.
		_, err := ReadPaste(pasteId, false)
		if err == nil {
			continue
		}

		if errors.Is(err, ErrPasteExpired) || errors.Is(err, ErrPasteNotFound) {
			slog.Debug("[SCHEDULER] paste deleted", "paste_id", pasteId)
			continue
		}

		slog.Error("[SCHEDULER] fail to check paste", "paste_id", pasteId, "err", err)
	}

	return nil
}

// InitScheduler registers the nightly cleanup pass. chrono only schedules the
// task, it does not block, so the caller keeps the error instead of dying in a
// goroutine the way the previous log.Fatalln did.
func InitScheduler() error {
	cron := config.Paste.Cron

	taskScheduler := chrono.NewDefaultTaskScheduler()

	_, err := taskScheduler.ScheduleWithCron(func(ctx context.Context) {
		if err := cleanDatas(); nil != err {
			slog.Error("[SCHEDULER] cleanDatas failed", "err", err)
		}
	}, cron)

	if nil != err {
		slog.Error("[SCHEDULER] fail to schedule cleanDatas", "cron", cron, "err", err)
		return err
	}

	slog.Info("[SCHEDULER] cleanDatas has been scheduled", "cron", cron)

	return nil
}
