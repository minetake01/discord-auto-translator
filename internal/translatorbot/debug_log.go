package translatorbot

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
)

// debugLogMaxBytes caps the debug log before it is rotated. Entries carry full
// prompts and responses, so an unbounded file would fill the deployment disk.
const debugLogMaxBytes = 64 << 20

// DebugLog appends one JSON object per line to a file for diagnosis and
// measurement. It is opt-in: nothing is written unless the operator
// configures a path. Entries contain message content, so the file is created
// with 0600.
type DebugLog struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func OpenDebugLog(path string) (*DebugLog, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("debug log path is required")
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &DebugLog{path: path, file: file, size: info.Size()}, nil
}

func (l *DebugLog) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// writeEntry appends entry as a single line. A failure to record diagnostics
// must not break translation, so it is surfaced on stderr instead.
func (l *DebugLog) writeEntry(entry any) {
	if l == nil {
		return
	}
	line, err := json.Marshal(entry)
	if err != nil {
		log.Printf("translation debug log: encode entry: %v", err)
		return
	}
	line = append(line, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size+int64(len(line)) > debugLogMaxBytes {
		if err := l.rotate(); err != nil {
			log.Printf("translation debug log: rotate %s: %v", l.path, err)
			return
		}
	}
	written, err := l.file.Write(line)
	l.size += int64(written)
	if err != nil {
		log.Printf("translation debug log: write %s: %v", l.path, err)
	}
}

func (l *DebugLog) rotate() error {
	if err := l.file.Close(); err != nil {
		return err
	}
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	l.file, l.size = file, 0
	return nil
}
