package main

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buger/goreplay/size"
)

var dateFileNameFuncs = map[string]func(*FileOutput) string{
	"%Y":  func(o *FileOutput) string { return time.Now().Format("2006") },
	"%m":  func(o *FileOutput) string { return time.Now().Format("01") },
	"%d":  func(o *FileOutput) string { return time.Now().Format("02") },
	"%H":  func(o *FileOutput) string { return time.Now().Format("15") },
	"%M":  func(o *FileOutput) string { return time.Now().Format("04") },
	"%S":  func(o *FileOutput) string { return time.Now().Format("05") },
	"%NS": func(o *FileOutput) string { return fmt.Sprint(time.Now().Nanosecond()) },
	"%r":  func(o *FileOutput) string { return string(o.currentID) },
	"%t":  func(o *FileOutput) string { return string(o.payloadType) },
}

// FileOutputConfig ...
type FileOutputConfig struct {
	FlushInterval     time.Duration `json:"output-file-flush-interval" mapstructure:"output-file-flush-interval"`
	SizeLimit         size.Size     `json:"output-file-size-limit" mapstructure:"output-file-size-limit"`
	OutputFileMaxSize size.Size     `json:"output-file-max-size-limit" mapstructure:"output-file-max-size-limit"`
	QueueLimit        int           `json:"output-file-queue-limit" mapstructure:"output-file-queue-limit"`
	Append            bool          `json:"output-file-append" mapstructure:"output-file-append"`
	BufferPath        string        `json:"output-file-buffer" mapstructure:"output-file-buffer"`
	onClose           func(string)
}

// FileOutput output plugin
type FileOutput struct {
	sync.RWMutex
	pathTemplate   string
	currentName    string
	file           *os.File
	QueueLength    int
	chunkSize      int
	writer         io.Writer
	requestPerFile bool
	currentID      []byte
	payloadType    []byte
	closed         bool
	totalFileSize  size.Size
	Service        string

	config *FileOutputConfig
}

// NewFileOutput constructor for FileOutput, accepts path
func NewFileOutput(pathTemplate string, config *FileOutputConfig) *FileOutput {
	o := new(FileOutput)
	o.pathTemplate = pathTemplate
	o.config = config
	o.updateName()

	if strings.Contains(pathTemplate, "%r") {
		o.requestPerFile = true
	}

	if config.FlushInterval == 0 {
		config.FlushInterval = 100 * time.Millisecond
	}

	if config.QueueLimit == 0 {
		config.QueueLimit = 256
	}

	if config.SizeLimit < 1 {
		config.SizeLimit = 33554432 // 32mb
	}

	if config.OutputFileMaxSize < 1 {
		config.OutputFileMaxSize = 1099511627776
	}

	go func() {
		for {
			time.Sleep(config.FlushInterval)
			if o.IsClosed() {
				break
			}
			o.updateName()
			o.flush()
		}
	}()

	return o
}

func getFileIndex(name string) int {
	ext := filepath.Ext(name)
	withoutExt := strings.TrimSuffix(name, ext)

	if idx := strings.LastIndex(withoutExt, "_"); idx != -1 {
		if i, err := strconv.Atoi(withoutExt[idx+1:]); err == nil {
			return i
		}
	}

	return -1
}

func setFileIndex(name string, idx int) string {
	idxS := strconv.Itoa(idx)
	ext := filepath.Ext(name)
	withoutExt := strings.TrimSuffix(name, ext)

	if i := strings.LastIndex(withoutExt, "_"); i != -1 {
		if _, err := strconv.Atoi(withoutExt[i+1:]); err == nil {
			withoutExt = withoutExt[:i]
		}
	}

	return withoutExt + "_" + idxS + ext
}

func withoutIndex(s string) string {
	if i := strings.LastIndex(s, "_"); i != -1 {
		return s[:i]
	}

	return s
}

type sortByFileIndex []string

func (s sortByFileIndex) Len() int {
	return len(s)
}

func (s sortByFileIndex) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sortByFileIndex) Less(i, j int) bool {
	if withoutIndex(s[i]) == withoutIndex(s[j]) {
		return getFileIndex(s[i]) < getFileIndex(s[j])
	}

	return s[i] < s[j]
}

func (o *FileOutput) filename() string {
	o.RLock()
	defer o.RUnlock()

	path := o.pathTemplate

	for name, fn := range dateFileNameFuncs {
		path = strings.Replace(path, name, fn(o), -1)
	}

	if !o.config.Append {
		nextChunk := false

		if o.currentName == "" ||
			((o.config.QueueLimit > 0 && o.QueueLength >= o.config.QueueLimit) ||
				(o.config.SizeLimit > 0 && o.chunkSize >= int(o.config.SizeLimit))) {
			nextChunk = true
		}

		ext := filepath.Ext(path)
		withoutExt := strings.TrimSuffix(path, ext)

		if matches, err := filepath.Glob(withoutExt + "*" + ext); err == nil {
			if len(matches) == 0 {
				return setFileIndex(path, 0)
			}
			sort.Sort(sortByFileIndex(matches))

			last := matches[len(matches)-1]

			fileIndex := 0
			if idx := getFileIndex(last); idx != -1 {
				fileIndex = idx

				if nextChunk {
					fileIndex++
				}
			}

			return setFileIndex(last, fileIndex)
		}
	}

	return path
}

func (o *FileOutput) updateName() {
	name := filepath.Clean(o.filename())
	o.Lock()
	o.currentName = name
	o.Unlock()
}

// PluginWrite writes message to this plugin
func (o *FileOutput) PluginWrite(msg *Message) (n int, err error) {
	if o.requestPerFile {
		o.Lock()
		meta := payloadMeta(msg.Meta)
		o.currentID = meta[1]
		o.payloadType = meta[0]
		o.Unlock()
	}

	o.updateName()
	o.Lock()
	defer o.Unlock()

	if o.file == nil || o.currentName != o.file.Name() {
		o.closeLocked()

		o.file, err = os.OpenFile(o.currentName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
		o.file.Sync()

		if strings.HasSuffix(o.currentName, ".gz") {
			o.writer = gzip.NewWriter(o.file)
		} else {
			o.writer = bufio.NewWriter(o.file)
		}

		if err != nil {
			log.Fatal(o, "Cannot open file %q. Error: %s", o.currentName, err)
		}

		o.QueueLength = 0
	}

	var nn int
	n, err = o.writer.Write(msg.Meta)
	nn, err = o.writer.Write(msg.Data)
	n += nn
	nn, err = o.writer.Write(payloadSeparatorAsBytes)
	n += nn

	o.totalFileSize += size.Size(n)
	o.QueueLength++

	if Settings.OutputFileConfig.OutputFileMaxSize > 0 && o.totalFileSize >= Settings.OutputFileConfig.OutputFileMaxSize {
		return n, errors.New("File output reached size limit")
	}

	return n, err
}

func (o *FileOutput) flush() {
	// Don't exit on panic
	defer func() {
		if r := recover(); r != nil {
			Debug(0, "[OUTPUT-FILE] PANIC while file flush: ", r, o, string(debug.Stack()))
		}
	}()

	o.Lock()
	defer o.Unlock()

	if o.file != nil {
		if strings.HasSuffix(o.currentName, ".gz") {
			o.writer.(*gzip.Writer).Flush()
		} else {
			o.writer.(*bufio.Writer).Flush()
		}

		if stat, err := o.file.Stat(); err == nil {
			o.chunkSize = int(stat.Size())
		} else {
			Debug(0, "[OUTPUT-HTTP] error accessing file size", err)
		}
	}
}

func (o *FileOutput) String() string {
	return "File output: " + o.pathTemplate
}

func (o *FileOutput) closeLocked() error {
	if o.file != nil {
		if strings.HasSuffix(o.currentName, ".gz") {
			o.writer.(*gzip.Writer).Close()
		} else {
			o.writer.(*bufio.Writer).Flush()
		}
		o.file.Close()

		if o.config.onClose != nil {
			o.config.onClose(o.file.Name())
		}
	}

	o.closed = true
	return nil
}

// Close closes the output file that is being written to.
func (o *FileOutput) Close() error {
	o.Lock()
	defer o.Unlock()
	return o.closeLocked()
}

// IsClosed returns if the output file is closed or not.
func (o *FileOutput) IsClosed() bool {
	o.Lock()
	defer o.Unlock()
	return o.closed
}
