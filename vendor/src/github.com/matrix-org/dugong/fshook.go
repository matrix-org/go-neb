package dugong

import (
	"fmt"
	"os"
	"sync/atomic"

	log "github.com/Sirupsen/logrus"
)

// NewFSHook makes a logging hook that writes JSON formatted
// log entries to info, warn and error log files. Each log file
// contains the messages with that severity or higher.
func NewFSHook(infoPath, warnPath, errorPath string) log.Hook {
	hook := &fsHook{
		entries:   make(chan log.Entry, 1024),
		infoPath:  infoPath,
		warnPath:  warnPath,
		errorPath: errorPath,
	}

	go func() {
		for entry := range hook.entries {
			if err := hook.writeEntry(&entry); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing to logfile: %v\n", err)
			}
			atomic.AddInt32(&hook.queueSize, -1)
		}
	}()

	return hook
}

type fsHook struct {
	entries   chan log.Entry
	queueSize int32
	infoPath  string
	warnPath  string
	errorPath string
	formatter log.JSONFormatter
}

func (hook *fsHook) Fire(entry *log.Entry) error {
	atomic.AddInt32(&hook.queueSize, 1)
	hook.entries <- *entry
	return nil
}

func (hook *fsHook) writeEntry(entry *log.Entry) error {
	msg, err := hook.formatter.Format(entry)
	if err != nil {
		return nil
	}

	if entry.Level <= log.ErrorLevel {
		if err := logToFile(hook.errorPath, msg); err != nil {
			return err
		}
	}

	if entry.Level <= log.WarnLevel {
		if err := logToFile(hook.warnPath, msg); err != nil {
			return err
		}
	}

	if entry.Level <= log.InfoLevel {
		if err := logToFile(hook.infoPath, msg); err != nil {
			return err
		}
	}

	return nil
}

func (hook *fsHook) Levels() []log.Level {
	return []log.Level{
		log.PanicLevel,
		log.FatalLevel,
		log.ErrorLevel,
		log.WarnLevel,
		log.InfoLevel,
	}
}

func logToFile(path string, msg []byte) error {
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer fd.Close()
	_, err = fd.Write(msg)
	return err
}
