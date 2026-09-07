package main

import (
    "errors"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"
    "time"
)

// useTempPasteFolder points the storage at a directory of its own for the
// duration of the test.
func useTempPasteFolder(t *testing.T) string {
    t.Helper()

    folder := t.TempDir()
    t.Setenv("OPEN_GO_PASTE_DATAS_FOLDER", folder)

    previousFolder := pasteFolder
    pasteFolder = ""
    t.Cleanup(func() {
        pasteFolder = previousFolder
    })

    return folder
}

// writeTestPaste stores a paste as is, bypassing CreatePaste, to build states
// that the nominal path cannot produce: an already expired paste, a counter
// close to its allowance.
func writeTestPaste(t *testing.T, paste Paste) string {
    t.Helper()

    pasteFilename := generatePasteFilename(paste.Id)

    if err := os.MkdirAll(filepath.Dir(pasteFilename), 0700); nil != err {
        t.Fatalf("fail to create the paste directory: %s", err)
    }

    if err := writePasteFile(paste, pasteFilename); nil != err {
        t.Fatalf("fail to write the paste: %s", err)
    }

    return pasteFilename
}

// The validation lists and the displayed lists are declared separately so that
// gotext can extract the labels: they must not drift apart.
func TestAllowedDurationsMatchDisplayedDurations(t *testing.T) {
    request := httptest.NewRequest(http.MethodGet, "/", nil)
    locale := NewLocale(request)

    var maxTime MaxTime
    displayed := maxTime.GetAllowed(locale)

    if len(displayed) != len(allowedDurations) {
        t.Fatalf("got %d displayed durations, want %d", len(displayed), len(allowedDurations))
    }

    for i, duration := range displayed {
        if duration.DurationString != allowedDurations[i] {
            t.Errorf("position %d: displayed %q, allowed %q", i, duration.DurationString, allowedDurations[i])
        }

        if !maxTime.HasAllowed(duration.DurationString) {
            t.Errorf("displayed duration %q is rejected by HasAllowed", duration.DurationString)
        }

        if duration.Label == "" {
            t.Errorf("duration %q has no label", duration.DurationString)
        }
    }

    if maxTime.HasAllowed("42y") {
        t.Error("HasAllowed accepts an unknown duration")
    }
}

func TestAllowedPasteTypesMatchDisplayedTypes(t *testing.T) {
    request := httptest.NewRequest(http.MethodGet, "/", nil)
    locale := NewLocale(request)

    var pasteType PasteType
    displayed := pasteType.GetAllowed(locale)

    if len(displayed) != len(allowedPasteTypes) {
        t.Fatalf("got %d displayed types, want %d", len(displayed), len(allowedPasteTypes))
    }

    for i, pasteTypeStruct := range displayed {
        if pasteTypeStruct.Value != allowedPasteTypes[i] {
            t.Errorf("position %d: displayed %q, allowed %q", i, pasteTypeStruct.Value, allowedPasteTypes[i])
        }

        if !pasteType.HasAllowed(pasteTypeStruct.Value) {
            t.Errorf("displayed type %q is rejected by HasAllowed", pasteTypeStruct.Value)
        }

        if pasteTypeStruct.Label == "" {
            t.Errorf("type %q has no label", pasteTypeStruct.Value)
        }
    }

    if pasteType.HasAllowed("binary") {
        t.Error("HasAllowed accepts an unknown type")
    }
}

func TestReadPasteReturnsSentinelErrors(t *testing.T) {
    useTempPasteFolder(t)

    if _, err := ReadPaste("not-an-uuid", false); err != ErrPasteNotFound {
        t.Errorf("got %v, want ErrPasteNotFound", err)
    }

    if _, err := ReadPaste("2c5ea4c0-4067-11e9-8bad-9b1deb4d3b7d", false); err != ErrPasteNotFound {
        t.Errorf("got %v, want ErrPasteNotFound", err)
    }
}

func TestGetExpirationTimeRejectsUnknownDuration(t *testing.T) {
    var maxTime MaxTime

    // An unknown duration used to return time.Now() without an error, which
    // creates a paste already expired.
    if _, err := maxTime.GetExpirationTime("42y"); nil == err {
        t.Error("an unknown duration is accepted")
    }

    for _, duration := range allowedDurations {
        expirationDate, err := maxTime.GetExpirationTime(duration)
        if nil != err {
            t.Errorf("duration %q: unexpected error %s", duration, err)
            continue
        }

        if !expirationDate.After(time.Now()) {
            t.Errorf("duration %q: expiration date is in the past", duration)
        }
    }
}

func TestCreatePasteRejectsUnknownExpiration(t *testing.T) {
    useTempPasteFolder(t)

    if _, err := CreatePaste("1,2,3", "text", "42y", 1); nil == err {
        t.Error("CreatePaste accepts an unknown expiration")
    }
}

func TestCreateThenReadPaste(t *testing.T) {
    useTempPasteFolder(t)

    pasteId, err := CreatePaste("1,2,3", "code", "1h", 0)
    if nil != err {
        t.Fatalf("fail to create the paste: %s", err)
    }

    paste, err := ReadPaste(pasteId, true)
    if nil != err {
        t.Fatalf("fail to read the paste: %s", err)
    }

    if paste.Data != "1,2,3" || paste.Type != "code" {
        t.Errorf("got data %q and type %q, want %q and %q", paste.Data, paste.Type, "1,2,3", "code")
    }

    if paste.NumberOfRead != 1 {
        t.Errorf("got %d read, want 1", paste.NumberOfRead)
    }

    // An unlimited paste survives its reads, and the counter is persisted.
    paste, err = ReadPaste(pasteId, true)
    if nil != err {
        t.Fatalf("fail to read the paste a second time: %s", err)
    }

    if paste.NumberOfRead != 2 {
        t.Errorf("got %d read, want 2", paste.NumberOfRead)
    }
}

func TestReadPasteBurnsAfterTheAllowedNumberOfRead(t *testing.T) {
    useTempPasteFolder(t)

    pasteId, err := CreatePaste("1,2,3", "text", "1h", 1)
    if nil != err {
        t.Fatalf("fail to create the paste: %s", err)
    }

    if _, err := ReadPaste(pasteId, true); nil != err {
        t.Fatalf("the first read must succeed: %s", err)
    }

    // The last allowed read destroys the paste, and the destruction must not be
    // undone by a rewrite.
    if _, err := ReadPaste(pasteId, true); !errors.Is(err, ErrPasteNotFound) {
        t.Errorf("got %v, want ErrPasteNotFound", err)
    }

    if _, err := os.Stat(generatePasteFilename(pasteId)); !os.IsNotExist(err) {
        t.Error("the paste file survived its last allowed read")
    }
}

// TestConcurrentReadsConsumeASingleAllowedRead is the regression test for the
// race on the read counter: both requests used to read NumberOfRead at 0 and
// both got served. Run it with -race.
func TestConcurrentReadsConsumeASingleAllowedRead(t *testing.T) {
    useTempPasteFolder(t)

    pasteId, err := CreatePaste("1,2,3", "text", "1h", 1)
    if nil != err {
        t.Fatalf("fail to create the paste: %s", err)
    }

    const readerCount = 20

    var waitGroup sync.WaitGroup
    var mutex sync.Mutex
    servedCount := 0

    for i := 0; i < readerCount; i++ {
        waitGroup.Add(1)

        go func() {
            defer waitGroup.Done()

            if _, err := ReadPaste(pasteId, true); nil == err {
                mutex.Lock()
                servedCount++
                mutex.Unlock()
            }
        }()
    }

    waitGroup.Wait()

    if servedCount != 1 {
        t.Errorf("%d readers were served a paste allowed a single read, want 1", servedCount)
    }
}

func TestReadPasteDeletesAnExpiredPaste(t *testing.T) {
    useTempPasteFolder(t)

    pasteId := "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
    pasteFilename := writeTestPaste(t, Paste{
        Id:         pasteId,
        ExpireTime: time.Now().Add(-time.Hour),
        Type:       "text",
        Data:       "1,2,3",
    })

    if _, err := ReadPaste(pasteId, true); !errors.Is(err, ErrPasteExpired) {
        t.Errorf("got %v, want ErrPasteExpired", err)
    }

    if _, err := os.Stat(pasteFilename); !os.IsNotExist(err) {
        t.Error("the expired paste file has not been deleted")
    }
}

func TestReadPasteReportsACorruptedFile(t *testing.T) {
    folder := useTempPasteFolder(t)

    pasteId := "3f2504e0-4f89-11d3-9a0c-0305e82c3302"
    pasteFilename := generatePasteFilename(pasteId)

    if err := os.MkdirAll(filepath.Dir(pasteFilename), 0700); nil != err {
        t.Fatalf("fail to create the paste directory: %s", err)
    }

    if err := os.WriteFile(pasteFilename, []byte("{not json"), 0600); nil != err {
        t.Fatalf("fail to write the paste: %s", err)
    }

    // A corrupted file used to unmarshal into a zeroed Paste, hence "expired",
    // hence a 404 hiding a storage problem.
    _, err := ReadPaste(pasteId, true)
    if nil == err {
        t.Fatal("a corrupted paste is read without an error")
    }

    if errors.Is(err, ErrPasteNotFound) || errors.Is(err, ErrPasteExpired) {
        t.Errorf("got the sentinel %v, want a parsing error", err)
    }

    if !strings.Contains(err.Error(), folder) {
        t.Errorf("the error does not name the file: %s", err)
    }
}

func TestReadPasteDoesNotRewriteAPasteItDoesNotChange(t *testing.T) {
    useTempPasteFolder(t)

    pasteId, err := CreatePaste("1,2,3", "text", "1h", 0)
    if nil != err {
        t.Fatalf("fail to create the paste: %s", err)
    }

    pasteFilename := generatePasteFilename(pasteId)
    modificationTime := time.Now().Add(-24 * time.Hour)

    if err := os.Chtimes(pasteFilename, modificationTime, modificationTime); nil != err {
        t.Fatalf("fail to set the modification time: %s", err)
    }

    // A crawler, and the nightly cleaning, must leave the file untouched.
    if _, err := ReadPaste(pasteId, false); nil != err {
        t.Fatalf("fail to read the paste: %s", err)
    }

    info, err := os.Stat(pasteFilename)
    if nil != err {
        t.Fatalf("fail to stat the paste: %s", err)
    }

    if !info.ModTime().Equal(modificationTime) {
        t.Error("the paste has been rewritten although nothing changed")
    }
}

func TestCleanDatasRemovesExpiredPastesOnly(t *testing.T) {
    useTempPasteFolder(t)

    expiredId := "3f2504e0-4f89-11d3-9a0c-0305e82c3303"
    expiredFilename := writeTestPaste(t, Paste{
        Id:         expiredId,
        ExpireTime: time.Now().Add(-time.Hour),
        Type:       "text",
        Data:       "1,2,3",
    })

    validId, err := CreatePaste("4,5,6", "text", "1Y", 1)
    if nil != err {
        t.Fatalf("fail to create the paste: %s", err)
    }

    if err := cleanDatas(); nil != err {
        t.Fatalf("cleanDatas failed: %s", err)
    }

    if _, err := os.Stat(expiredFilename); !os.IsNotExist(err) {
        t.Error("the expired paste survived the cleaning")
    }

    // The valid paste keeps its allowed read: the cleaning must not consume it.
    paste, err := ReadPaste(validId, false)
    if nil != err {
        t.Fatalf("the valid paste did not survive the cleaning: %s", err)
    }

    if paste.NumberOfRead != 0 {
        t.Errorf("got %d read after the cleaning, want 0", paste.NumberOfRead)
    }
}
