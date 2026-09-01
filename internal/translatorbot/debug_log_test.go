package translatorbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestOpenDebugLogRequiresPath(t *testing.T) {
	if _, err := OpenDebugLog("   "); err == nil {
		t.Fatal("expected error")
	}
}

func TestDebugLogAppendsOneJSONLinePerEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "translation-debug.log")
	debugLog, err := OpenDebugLog(path)
	if err != nil {
		t.Fatal(err)
	}
	debugLog.writeEntry(map[string]string{"first": "line\nwith newline"})
	debugLog.writeEntry(map[string]string{"second": "line"})
	if err := debugLog.Close(); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("permissions = %v, want 0600", info.Mode().Perm())
		}
	}
	lines := readDebugLogLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("line is not JSON: %s", line)
		}
	}
	if !strings.Contains(lines[0], `"line\nwith newline"`) {
		t.Fatalf("first line = %s", lines[0])
	}
}

func TestDebugLogReopensAfterExistingFileGrowsPastMaxBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "translation-debug.log")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, debugLogMaxBytes); err != nil {
		t.Fatal(err)
	}
	debugLog, err := OpenDebugLog(path)
	if err != nil {
		t.Fatal(err)
	}
	debugLog.writeEntry(map[string]string{"event": "after rotate"})
	if err := debugLog.Close(); err != nil {
		t.Fatal(err)
	}

	rotated, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Size() != debugLogMaxBytes {
		t.Fatalf("rotated size = %d, want %d", rotated.Size(), debugLogMaxBytes)
	}
	if lines := readDebugLogLines(t, path); len(lines) != 1 || !strings.Contains(lines[0], "after rotate") {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestDebugLogKeepsConcurrentEntriesIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "translation-debug.log")
	debugLog, err := OpenDebugLog(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			debugLog.writeEntry(map[string]int{"index": i})
		}()
	}
	wg.Wait()
	if err := debugLog.Close(); err != nil {
		t.Fatal(err)
	}

	lines := readDebugLogLines(t, path)
	if len(lines) != 20 {
		t.Fatalf("lines = %d, want 20", len(lines))
	}
	seen := make(map[int]bool, len(lines))
	for _, line := range lines {
		var entry struct{ Index int }
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %s: %v", line, err)
		}
		seen[entry.Index] = true
	}
	if len(seen) != 20 {
		t.Fatalf("distinct entries = %d, want 20", len(seen))
	}
}

func TestNilDebugLogIsInert(t *testing.T) {
	var debugLog *DebugLog
	debugLog.writeEntry(map[string]string{"ignored": "entry"})
	if err := debugLog.Close(); err != nil {
		t.Fatal(err)
	}
}

func readDebugLogLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSuffix(string(data), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
