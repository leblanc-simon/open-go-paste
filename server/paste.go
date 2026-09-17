package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"leblanc.io/open-go-base/i18n"
)

// pasteFolder is resolved once by InitPasteFolder: neither the environment nor
// the working directory are expected to change while the server runs.
var pasteFolder string

var (
	// ErrPasteNotFound is returned when the identifier matches no stored paste.
	ErrPasteNotFound = errors.New("paste not found")
	// ErrPasteExpired is returned when the paste is past its expiration date.
	ErrPasteExpired = errors.New("paste expired")
)

// pasteLocks serialises the read, increment and write cycle of ReadPaste. The
// locks are sharded over a fixed table rather than allocated per identifier,
// which would grow without bound; a collision between two identifiers only
// costs a serialised write. This guards a single process, not several instances
// sharing the same storage.
const pasteLockCount = 256

var pasteLocks [pasteLockCount]sync.Mutex

func pasteLock(id string) *sync.Mutex {
	hash := fnv.New32a()
	hash.Write([]byte(id))

	return &pasteLocks[hash.Sum32()%pasteLockCount]
}

type Paste struct {
	Id                  string    `json:"id"`
	ExpireTime          time.Time `json:"expire"`
	NumberOfRead        int       `json:"number_of_read"`
	AllowedNumberOfRead int       `json:"allowed_number_of_read"`
	Type                string    `json:"type"`
	Data                string    `json:"data"`
}

type DurationTime struct {
	DurationString string
	Label          string
	Default        bool
}

type MaxTime struct {
}

// allowedDurations is the source of truth for validation and for the form. The
// list used to be repeated inside GetAllowed so that gotext could extract the
// English labels; the labels now live in the catalogues under the
// "duration.<value>" keys, so one list is enough.
var allowedDurations = []string{"5m", "10m", "1h", "1D", "1W", "1M", "1Y"}

// defaultDuration is the entry preselected in the form.
const defaultDuration = "1D"

func (mt MaxTime) GetAllowed(locale *i18n.Localizer) []DurationTime {
	maxTimes := make([]DurationTime, 0, len(allowedDurations))

	for _, duration := range allowedDurations {
		maxTimes = append(maxTimes, DurationTime{
			DurationString: duration,
			Label:          locale.T("duration." + duration),
			Default:        defaultDuration == duration,
		})
	}

	return maxTimes
}

func (mt MaxTime) GetExpirationTime(wantedExpiration string) (time.Time, error) {
	expirationDate := time.Now()
	switch wantedExpiration {
	case "5m", "10m", "1h":
		durationAdded, err := time.ParseDuration(wantedExpiration)
		if nil != err {
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

	// Without this guard an unknown duration silently expires the paste at its
	// creation. HasAllowed protects the controller, CreatePaste stays exported.
	return expirationDate, fmt.Errorf("unknown expiration %q", wantedExpiration)
}

func (mt MaxTime) HasAllowed(maxTime string) bool {
	for _, allowed := range allowedDurations {
		if allowed == maxTime {
			return true
		}
	}

	return false
}

type PasteTypeStruct struct {
	Value   string
	Label   string
	Default bool
}

type PasteType struct {
}

// allowedPasteTypes is the source of truth for validation and for the form, its
// labels being the "paste_type.<value>" keys of the catalogues.
var allowedPasteTypes = []string{"text", "code", "markdown"}

// defaultPasteType is the entry preselected in the form.
const defaultPasteType = "text"

func (pt PasteType) GetAllowed(locale *i18n.Localizer) []PasteTypeStruct {
	types := make([]PasteTypeStruct, 0, len(allowedPasteTypes))

	for _, pasteType := range allowedPasteTypes {
		types = append(types, PasteTypeStruct{
			Value:   pasteType,
			Label:   locale.T("paste_type." + pasteType),
			Default: defaultPasteType == pasteType,
		})
	}

	return types
}

func (pt PasteType) HasAllowed(pasteType string) bool {
	for _, allowed := range allowedPasteTypes {
		if allowed == pasteType {
			return true
		}
	}

	return false
}

func resolvePasteFolder() string {
	if configured := config.Paste.DatasFolder; "" != configured {
		return configured
	}

	cwd, err := os.Getwd()
	if nil != err {
		slog.Error("fail to get working directory", "err", err)
		return filepath.Join("..", "datas")
	}

	return filepath.Join(cwd, "..", "datas")
}

// getPasteFolder returns the folder resolved at startup. It falls back to
// resolving it on the spot so that callers which skip InitPasteFolder, the tests
// among them, still address the same storage.
func getPasteFolder() string {
	if "" == pasteFolder {
		return resolvePasteFolder()
	}

	return pasteFolder
}

func InitPasteFolder() {
	pasteFolder = resolvePasteFolder()

	if _, err := os.Stat(pasteFolder); os.IsNotExist(err) {
		// slog has no Fatal: the service cannot serve a single paste without
		// its storage, so the exit stays here rather than being returned.
		slog.Error("paste folder doesn't exist", "folder", pasteFolder)
		os.Exit(1)
	}

	slog.Info("paste folder resolved", "folder", pasteFolder)
}

func generatePasteId() string {
	for {
		pasteUuid, _ := uuid.NewRandom()
		pasteId := pasteUuid.String()
		if !isPasteFileExist(pasteId) {
			// Debug and not info: the identifier is half of a paste url, it has
			// no place in the journal an operator keeps by default.
			slog.Debug("paste id generated", "paste_id", pasteId)
			return pasteId
		}
	}
}

func isValidUUID(u string) bool {
	_, err := uuid.Parse(u)
	return err == nil
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

	return currentTime.After(paste.ExpireTime)
}

// isPasteFullyRead tells whether the allowed number of reads is exhausted. A
// zero allowance means unlimited.
func isPasteFullyRead(paste Paste) bool {
	return paste.AllowedNumberOfRead > 0 && paste.NumberOfRead >= paste.AllowedNumberOfRead
}

func writePasteFile(paste Paste, pasteFilename string) error {
	pasteJson, err := json.Marshal(paste)
	if nil != err {
		return fmt.Errorf("fail to serialize paste %s: %w", paste.Id, err)
	}

	if err := os.WriteFile(pasteFilename, pasteJson, 0600); nil != err {
		return fmt.Errorf("fail to write file %s: %w", pasteFilename, err)
	}

	return nil
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

	if nil != err {
		return "", err
	}

	pasteFilename := generatePasteFilename(paste.Id)
	pasteDirectory := filepath.Dir(pasteFilename)

	if err := os.MkdirAll(pasteDirectory, 0700); nil != err {
		return "", fmt.Errorf("fail to create directory %s: %w", pasteDirectory, err)
	}

	// The caller must never receive an identifier that points to nothing: a dead
	// link is indistinguishable from an expired paste for the user.
	if err := writePasteFile(paste, pasteFilename); nil != err {
		return "", err
	}

	return paste.Id, nil
}

func ReadPaste(id string, isValidUserAgent bool) (Paste, error) {
	var paste Paste

	if !isValidUUID(id) {
		return paste, ErrPasteNotFound
	}

	pasteFilename := generatePasteFilename(id)

	// The read, increment and write cycle has to be atomic: two concurrent
	// requests on a paste allowed a single read would otherwise both see
	// NumberOfRead at 0 and both get served.
	lock := pasteLock(id)
	lock.Lock()
	defer lock.Unlock()

	pasteJson, err := os.ReadFile(pasteFilename)
	if nil != err {
		if os.IsNotExist(err) {
			return paste, ErrPasteNotFound
		}

		return paste, fmt.Errorf("fail to read file %s: %w", pasteFilename, err)
	}

	// A corrupted file used to yield a zeroed Paste, therefore "expired",
	// therefore a misleading 404.
	if err := json.Unmarshal(pasteJson, &paste); nil != err {
		return paste, fmt.Errorf("fail to parse file %s: %w", pasteFilename, err)
	}

	if isValidUserAgent {
		// Increment number of read only if we have a valid user-agent
		paste.NumberOfRead = paste.NumberOfRead + 1
	}

	if err := deleteIfNecessary(paste, pasteFilename, isValidUserAgent); nil != err {
		// Serving a paste whose counter could not be persisted would grant
		// unlimited reads on a paste meant to be destroyed.
		return paste, err
	}

	if isPasteIsExpired(paste) {
		return paste, ErrPasteExpired
	}

	return paste, nil
}

// deleteIfNecessary applies the outcome of a read: an expired or fully read
// paste is destroyed, otherwise the updated counter is written back. Never both,
// where the previous version relied on removePasteFile having pruned the parent
// directories to make the unconditional rewrite fail. And nothing at all when
// the counter did not move, which used to rewrite every valid paste on each
// nightly pass.
func deleteIfNecessary(paste Paste, pasteFilename string, counterChanged bool) error {
	if isPasteIsExpired(paste) || isPasteFullyRead(paste) {
		return removePasteFile(pasteFilename)
	}

	if !counterChanged {
		return nil
	}

	return writePasteFile(paste, pasteFilename)
}

func removePasteFile(pasteFilename string) error {
	if err := os.Remove(pasteFilename); nil != err {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("fail to remove file %s: %w", pasteFilename, err)
	}

	// Prune the now empty directories of the storage tree. os.Remove stops at
	// the first one still holding another paste, which is the expected end.
	pasteFolder := getPasteFolder()
	pathToRemove := pasteFilename

	for {
		pathToRemove = filepath.Dir(pathToRemove)
		if pasteFolder == pathToRemove {
			return nil
		}

		if err := os.Remove(pathToRemove); nil != err {
			return nil
		}
	}
}
