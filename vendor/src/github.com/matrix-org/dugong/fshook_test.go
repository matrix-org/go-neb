package dugong

import (
	"bufio"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	log "github.com/Sirupsen/logrus"
)

const (
	fieldName  = "my_field"
	fieldValue = "my_value"
)

func TestFSHookInfo(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	logger.WithField(fieldName, fieldValue).Info("Info message")

	wait()

	checkLogFile(t, hook.infoPath, "info")
}

func TestFSHookWarn(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	logger.WithField(fieldName, fieldValue).Warn("Warn message")

	wait()

	checkLogFile(t, hook.infoPath, "warning")
	checkLogFile(t, hook.warnPath, "warning")
}

func TestFSHookError(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	logger.WithField(fieldName, fieldValue).Error("Error message")

	wait()

	checkLogFile(t, hook.infoPath, "error")
	checkLogFile(t, hook.warnPath, "error")
	checkLogFile(t, hook.errorPath, "error")
}

func TestFsHookInterleaved(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	logger.WithField("counter", 0).Info("message")
	logger.WithField("counter", 1).Warn("message")
	logger.WithField("counter", 2).Error("message")
	logger.WithField("counter", 3).Warn("message")
	logger.WithField("counter", 4).Info("message")

	wait()

	file, err := os.Open(hook.infoPath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		data := make(map[string]interface{})
		if err := json.Unmarshal([]byte(scanner.Text()), &data); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		dataCounter := int(data["counter"].(float64))
		if count != dataCounter {
			t.Fatalf("Counter: want %d got %d", count, dataCounter)
		}
		count++
	}

	if count != 5 {
		t.Fatalf("Lines: want 5 got %d", count)
	}
}

func TestFSHookMultiple(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	for i := 0; i < 100; i++ {
		logger.WithField("counter", i).Info("message")
	}

	wait()

	file, err := os.Open(hook.infoPath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		data := make(map[string]interface{})
		if err := json.Unmarshal([]byte(scanner.Text()), &data); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		dataCounter := int(data["counter"].(float64))
		if count != dataCounter {
			t.Fatalf("Counter: want %d got %d", count, dataCounter)
		}
		count++
	}

	if count != 100 {
		t.Fatalf("Lines: want 100 got %d", count)
	}
}

func TestFSHookConcurrent(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func(counter int) {
			defer wg.Done()
			logger.WithField("counter", counter).Info("message")
		}(i)
	}

	wg.Wait()
	wait()

	file, err := os.Open(hook.infoPath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		data := make(map[string]interface{})
		if err := json.Unmarshal([]byte(scanner.Text()), &data); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		count++
	}

	if count != 100 {
		t.Fatalf("Lines: want 100 got %d", count)
	}
}

func setupLogHook(t *testing.T) (logger *log.Logger, hook *fsHook, wait func(), teardown func()) {
	dir, err := ioutil.TempDir("", "TestFSHook")
	if err != nil {
		t.Fatalf("Failed to make temporary directory: %v", err)
	}

	infoPath := filepath.Join(dir, "info.log")
	warnPath := filepath.Join(dir, "warn.log")
	errorPath := filepath.Join(dir, "error.log")

	hook = NewFSHook(infoPath, warnPath, errorPath).(*fsHook)

	logger = log.New()
	logger.Hooks.Add(hook)

	wait = func() {
		for atomic.LoadInt32(&hook.queueSize) != 0 {
			runtime.Gosched()
		}
	}

	teardown = func() {
		os.RemoveAll(dir)
	}

	return
}

func checkLogFile(t *testing.T, path, expectedLevel string) {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	data := make(map[string]interface{})
	if err := json.Unmarshal(contents, &data); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if data["level"] != expectedLevel {
		t.Fatalf("level: want %q got %q", expectedLevel, data["level"])
	}

	if data[fieldName] != fieldValue {
		t.Fatalf("%s: want %q got %q", fieldName, fieldValue, data[fieldName])
	}
}
