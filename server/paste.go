package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "io/ioutil"
    "log"
    "os"
    "path/filepath"
    "time"

    "github.com/google/uuid"
)

var pasteFolder string

type Paste struct {
    Id string `json:"id"`
    ExpireTime time.Time `json:"expire"`
    NumberOfRead int `json:"number_of_read"`
    AllowedNumberOfRead int `json:"allowed_number_of_read"`
    Type string `json:"type"`
    Data string `json:"data"`
}

type DurationTime struct {
    DurationString string
    Label string
    Default bool
}

type MaxTime struct {
}

func (mt MaxTime) GetAllowed() []DurationTime {
    maxTimes := []DurationTime{}
    maxTimes = append(maxTimes, DurationTime{DurationString: "5m", Label: "5 minutes", Default: false})
    maxTimes = append(maxTimes, DurationTime{DurationString: "10m", Label: "10 minutes", Default: false})
    maxTimes = append(maxTimes, DurationTime{DurationString: "1h", Label: "1 heure", Default: false})
    maxTimes = append(maxTimes, DurationTime{DurationString: "1D", Label: "1 jour", Default: true})
    maxTimes = append(maxTimes, DurationTime{DurationString: "1W", Label: "1 semaine", Default: false})
    maxTimes = append(maxTimes, DurationTime{DurationString: "1M", Label: "1 mois", Default: false})
    maxTimes = append(maxTimes, DurationTime{DurationString: "1Y", Label: "1 an", Default: false})

    return maxTimes;
    // return []string {"5 minutes", "10 minutes", "1 hour", "1 day", "1 week", "1 month", "1 year", "never"}
}

func (mt MaxTime) GetExpirationTime(wantedExpiration string) (time.Time, error) {
    expirationDate := time.Now()
    switch wantedExpiration {
    case "5m", "10m", "1h":
        durationAdded, err := time.ParseDuration(wantedExpiration)
        if (nil != err) {
            return expirationDate, err
        }
        expirationDate = expirationDate.Add(durationAdded)
        return expirationDate, nil
    case "1D":
        expirationDate := expirationDate.AddDate(0, 0, 1)
        return expirationDate, nil
    case "1W":
        expirationDate := expirationDate.AddDate(0, 0, 7)
        return expirationDate, nil
    case "1M":
        expirationDate := expirationDate.AddDate(0, 1, 0)
        return expirationDate, nil
    case "1Y":
        expirationDate := expirationDate.AddDate(1, 0, 0)
        return expirationDate, nil
    }

    return expirationDate, nil
}

func (mt MaxTime) HasAllowed(maxTime string) bool {
    for _, a := range mt.GetAllowed() {
        if (a.DurationString == maxTime) {
            return true
        }
    }

    return false
}

type PasteType struct {
}

func (pt PasteType) GetAllowed() []string {
    return []string {"text", "code", "markdown"}
}

func (pt PasteType) HasAllowed(pasteType string) bool {
    for _, a := range pt.GetAllowed() {
        if (a == pasteType) {
            return true
        }
    }

    return false
}

func getPasteFolder() string {
    envFolder := os.Getenv("OPEN_GO_PASTE_DATAS_FOLDER")
    if ("" != envFolder) {
        return envFolder
    }

    cwd, _ := os.Getwd()
    return filepath.Join(cwd, "..", "datas")
}

func InitPasteFolder() {
    pasteFolder := getPasteFolder()

    if _, err := os.Stat(pasteFolder); os.IsNotExist(err) {
        log.Fatalf("Paste folder %s doesn't exist", pasteFolder)
    }

    log.Printf("Paste folder will be %s", pasteFolder)
}

func generatePasteId() string {
    for {
        pasteUuid, _ := uuid.NewRandom()
        pasteId := pasteUuid.String()
        if (false == isPasteFileExist(pasteId)) {
            log.Printf("pasteId will be %s", pasteId)
            return pasteId
        }
    }
}

func generatePasteFilename(id string) string {
    return filepath.Join(getPasteFolder(), id[0:1], id[1:2], id[2:3], id[3:4], id[4:5], id[5:6], id[6:7], id[7:8], id)
}

func isPasteFileExist(id string) bool {
    filename := generatePasteFilename(id)
    if _, err := os.Stat(filename); os.IsNotExist(err) {
        return false
    }

    return true
}

func isPasteIsExpired(paste Paste) bool {
    currentTime := time.Now()

    return currentTime.After(paste.ExpireTime);
}

func CreatePaste(data string, pasteType string, finishTime string, numberOfRead int) (string, error) {
    var paste Paste
    var maxTime MaxTime
    var err error

    paste.Id = generatePasteId()
    paste.ExpireTime, err = maxTime.GetExpirationTime(finishTime)
    paste.NumberOfRead = 0
    paste.AllowedNumberOfRead = numberOfRead
    paste.Type = pasteType
    paste.Data = data

    if (nil != err) {
        return "", err
    }

    pasteJson, err := json.Marshal(paste)
    if (nil != err) {
        return "", err
    }

    pasteFilename := generatePasteFilename(paste.Id)
    pasteDirectory := filepath.Dir(pasteFilename)

    if _, err := os.Stat(pasteDirectory); os.IsNotExist(err) {
        os.MkdirAll(pasteDirectory, os.ModePerm)
    }

    ioutil.WriteFile(pasteFilename, pasteJson, 0600)

    return paste.Id, nil
}

func ReadPaste(id string) (Paste, error) {
    var paste Paste

    if (false == isPasteFileExist(id)) {
        return paste, errors.New("This paste doesn't exist")
    }

    pasteFilename := generatePasteFilename(id)

    pasteJson, err := ioutil.ReadFile(pasteFilename)
    if (nil != err) {
        return paste, errors.New(fmt.Sprintf("Fail to read file %s : %s", pasteFilename, err))
    }

    json.Unmarshal(pasteJson, &paste)

    paste.NumberOfRead = paste.NumberOfRead + 1

    deleteIfNecessary(paste, pasteFilename)

    if (true == isPasteIsExpired(paste)) {
        return paste, errors.New("This paste doesn't exist or is expired")
    }

    return paste, nil
}

func deleteIfNecessary(paste Paste, pasteFilename string) {
    if (paste.AllowedNumberOfRead > 0 && paste.NumberOfRead >= paste.AllowedNumberOfRead) {
        removePasteFile(pasteFilename)
    }

    if (true == isPasteIsExpired(paste)) {
        removePasteFile(pasteFilename)
    }

    pasteJson, err := json.Marshal(paste)
    if (nil != err) {
        // TODO: Process error
    }
    ioutil.WriteFile(pasteFilename, pasteJson, 0600)
}

func removePasteFile(pasteFilename string) {
    if _, err := os.Stat(pasteFilename); os.IsNotExist(err) {
        return
    }

    pasteFolder := getPasteFolder()
    pathToRemove := pasteFilename

    os.Remove(pasteFilename)

    for {
        pathToRemove = filepath.Dir(pathToRemove)
        if (pasteFolder == pathToRemove) {
            return;
        }
        err := os.Remove(pathToRemove)
        if (nil != err) {
            return;
        }
    }
}