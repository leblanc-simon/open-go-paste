package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
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
            log.Printf("[SCHEDULER] Skip %s : %s\n", path, err)
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
        log.Printf("[SCHEDULER] Check paste :%s\n", pasteId)

        // Still valid pastes are left untouched: the read counter does not move
        // for an invalid user-agent, so nothing is rewritten.
        _, err := ReadPaste(pasteId, false)
        if err == nil {
            continue
        }

        if errors.Is(err, ErrPasteExpired) || errors.Is(err, ErrPasteNotFound) {
            log.Printf("[SCHEDULER] Paste \"%s\" deleted\n", pasteId)
            continue
        }

        log.Printf("[SCHEDULER] Fail to check paste \"%s\" : %s\n", pasteId, err)
    }

    return nil
}

func InitScheduler() error {
    defaultCron := "0 0 0 * * *"
    envCron := os.Getenv("OPEN_GO_PASTE_CRON")
    var cron bytes.Buffer
    if envCron != "" {
        cron.WriteString(envCron)
    } else {
        cron.WriteString(defaultCron)
    }

    taskScheduler := chrono.NewDefaultTaskScheduler()

    _, err := taskScheduler.ScheduleWithCron(func(ctx context.Context) {
        err := cleanDatas()
        if err != nil {
            log.Println(err)
        }
    }, cron.String())

    if err == nil {
        log.Print("[SCHEDULER] cleanDatas has been scheduled : " + cron.String())
    } else {
        log.Print("[SCHEDULER] Fail to schedule cleanDatas : " + cron.String())
        log.Fatalln(err)
    }

    return err
}
