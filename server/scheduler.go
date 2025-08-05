package main

import (
	"bytes"
	"context"

	"log"
	"os"
	"path/filepath"

	"codnect.io/chrono"
)

func cleanDatas() error {
    err := filepath.Walk(getPasteFolder(), func(path string, info os.FileInfo, err error) error {
        if err != nil {
            log.Println(err)
            return err
        }

        if info.IsDir() {
            return nil
        }

        log.Printf("[SCHEDULER] Check paste :%s\n", info.Name())

        if !isValidUUID(info.Name()) {
            return nil
        }

        _, err = ReadPaste(info.Name(), false)
        if err != nil {
            log.Printf("[SCHEDULER] Paste \"%s\" deleted\n", info.Name())
        }

        return nil
    })

    return err
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
