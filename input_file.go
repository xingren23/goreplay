package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"container/heap"
	"errors"
	"expvar"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

type filePayload struct {
	data      []byte
	timestamp int64
}

// An IntHeap is a min-heap of ints.
type payloadQueue struct {
	sync.RWMutex
	s []*filePayload
}

func (h payloadQueue) Len() int           { return len(h.s) }
func (h payloadQueue) Less(i, j int) bool { return h.s[i].timestamp < h.s[j].timestamp }
func (h payloadQueue) Swap(i, j int)      { h.s[i], h.s[j] = h.s[j], h.s[i] }

func (h *payloadQueue) Push(x interface{}) {
	// Push and Pop use pointer receivers because they modify the slice's length,
	// not just its contents.
	h.s = append(h.s, x.(*filePayload))
}

func (h *payloadQueue) Pop() interface{} {
	old := h.s
	n := len(old)
	x := old[n-1]
	h.s = old[0 : n-1]
	return x
}

func (h payloadQueue) Idx(i int) *filePayload {
	return h.s[i]
}

type fileInputReader struct {
	reader    *bufio.Reader
	file      io.ReadCloser
	closed    int32 // Value of 0 indicates that the file is still open.
	s3        bool
	queue     payloadQueue
	readDepth int
}

func (f *fileInputReader) parse(init chan struct{}) error {
	payloadSeparatorAsBytes := []byte(payloadSeparator)
	var buffer bytes.Buffer
	var initialized bool

	for {
		line, err := f.reader.ReadBytes('\n')

		if err != nil {
			if err != io.EOF {
				Debug(1, err)
			}

			f.Close()

			if !initialized {
				close(init)
				initialized = true
			}

			return err
		}

		if bytes.Equal(payloadSeparatorAsBytes[1:], line) {
			asBytes := buffer.Bytes()
			meta := payloadMeta(asBytes)

			timestamp, _ := strconv.ParseInt(string(meta[2]), 10, 64)
			data := asBytes[:len(asBytes)-1]

			f.queue.Lock()
			heap.Push(&f.queue, &filePayload{
				timestamp: timestamp,
				data:      data,
			})
			f.queue.Unlock()

			for {
				if f.queue.Len() < f.readDepth {
					break
				}

				if !initialized {
					close(init)
					initialized = true
				}

				time.Sleep(100 * time.Millisecond)
			}

			buffer = bytes.Buffer{}
			continue
		}

		buffer.Write(line)
	}
}

func (f *fileInputReader) wait() {
	for {
		if atomic.LoadInt32(&f.closed) == 1 {
			return
		}

		if f.queue.Len() > 0 {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	return
}

// Close closes this plugin
func (f *fileInputReader) Close() error {
	if atomic.LoadInt32(&f.closed) == 0 {
		atomic.StoreInt32(&f.closed, 1)
		f.file.Close()
	}

	return nil
}

func newFileInputReader(path string, readDepth int) *fileInputReader {
	var file io.ReadCloser
	var err error

	if strings.HasPrefix(path, "s3://") {
		file = NewS3ReadCloser(path)
	} else {
		file, err = os.Open(path)
	}

	if err != nil {
		Debug(0, fmt.Sprintf("[INPUT-FILE] err: %q", err))
		return nil
	}

	r := &fileInputReader{file: file, closed: 0, readDepth: readDepth}
	if strings.HasSuffix(path, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			Debug(0, fmt.Sprintf("[INPUT-FILE] err: %q", err))
			return nil
		}
		r.reader = bufio.NewReader(gzReader)
	} else {
		r.reader = bufio.NewReader(file)
	}

	heap.Init(&r.queue)

	init := make(chan struct{})
	go r.parse(init)
	<-init

	return r
}

// FileInput can read requests generated by FileOutput
type FileInput struct {
	mu          sync.Mutex
	data        chan []byte
	exit        chan bool
	path        string
	readers     []*fileInputReader
	speedFactor float64
	loop        bool
	readDepth   int
	dryRun      bool

	stats *expvar.Map
}

// NewFileInput constructor for FileInput. Accepts file path as argument.
func NewFileInput(path string, loop bool, readDepth int, dryRun bool) (i *FileInput) {
	i = new(FileInput)
	i.data = make(chan []byte, 1000)
	i.exit = make(chan bool)
	i.path = path
	i.speedFactor = 1
	i.loop = loop
	i.readDepth = readDepth
	i.stats = expvar.NewMap("file-" + path)
	i.dryRun = dryRun

	if err := i.init(); err != nil {
		return
	}

	go i.emit()

	return
}

func (i *FileInput) init() (err error) {
	defer i.mu.Unlock()
	i.mu.Lock()

	var matches []string

	if strings.HasPrefix(i.path, "s3://") {
		sess := session.Must(session.NewSession(awsConfig()))
		svc := s3.New(sess)

		bucket, key := parseS3Url(i.path)

		params := &s3.ListObjectsInput{
			Bucket: aws.String(bucket),
			Prefix: aws.String(key),
		}

		resp, err := svc.ListObjects(params)
		if err != nil {
			Debug(0, "[INPUT-FILE] Error while retreiving list of files from S3", i.path, err)
			return err
		}

		for _, c := range resp.Contents {
			matches = append(matches, "s3://"+bucket+"/"+(*c.Key))
		}
	} else if matches, err = filepath.Glob(i.path); err != nil {
		Debug(0, "[INPUT-FILE] Wrong file pattern", i.path, err)
		return
	}

	if len(matches) == 0 {
		Debug(0, "[INPUT-FILE] No files match pattern: ", i.path)
		return errors.New("No matching files")
	}

	i.readers = make([]*fileInputReader, len(matches))

	for idx, p := range matches {
		i.readers[idx] = newFileInputReader(p, i.readDepth)
	}

	i.stats.Add("reader_count", int64(len(matches)))

	return nil
}

// PluginRead reads message from this plugin
func (i *FileInput) PluginRead() (*Message, error) {
	var msg Message
	select {
	case <-i.exit:
		return nil, ErrorStopped
	case buf := <-i.data:
		i.stats.Add("read_from", 1)
		msg.Meta, msg.Data = payloadMetaWithBody(buf)
		return &msg, nil
	}
}

func (i *FileInput) String() string {
	return "File input: " + i.path
}

// Find reader with smallest timestamp e.g next payload in row
func (i *FileInput) nextReader() (next *fileInputReader) {
	for _, r := range i.readers {
		if r == nil {
			continue
		}

		r.wait()

		if r.queue.Len() == 0 {
			continue
		}

		if next == nil || r.queue.Idx(0).timestamp < next.queue.Idx(0).timestamp {
			next = r
			continue
		}
	}

	return
}

func (i *FileInput) emit() {
	var lastTime int64 = -1

	var maxWait, firstWait, minWait int64
	minWait = math.MaxInt64

	i.stats.Add("negative_wait", 0)

	for {
		select {
		case <-i.exit:
			return
		default:
		}

		reader := i.nextReader()

		if reader == nil {
			if i.loop {
				i.init()
				lastTime = -1
				continue
			} else {
				break
			}
		}

		reader.queue.RLock()
		payload := heap.Pop(&reader.queue).(*filePayload)
		i.stats.Add("total_counter", 1)
		i.stats.Add("total_bytes", int64(len(payload.data)))
		reader.queue.RUnlock()

		if lastTime != -1 {
			diff := payload.timestamp - lastTime

			if firstWait == 0 {
				firstWait = diff
			}

			if i.speedFactor != 1 {
				diff = int64(float64(diff) / i.speedFactor)
			}

			if diff >= 0 {
				lastTime = payload.timestamp

				if !i.dryRun {
					time.Sleep(time.Duration(diff))
				}

				i.stats.Add("total_wait", diff)

				if diff > maxWait {
					maxWait = diff
				}

				if diff < minWait {
					minWait = diff
				}
			} else {
				i.stats.Add("negative_wait", 1)
			}
		} else {
			lastTime = payload.timestamp
		}

		// Recheck if we have exited since last check.
		select {
		case <-i.exit:
			return
		default:
			if !i.dryRun {
				i.data <- payload.data
			}
		}
	}

	i.stats.Set("first_wait", time.Duration(firstWait))
	i.stats.Set("max_wait", time.Duration(maxWait))
	i.stats.Set("min_wait", time.Duration(minWait))

	Debug(0, fmt.Sprintf("[INPUT-FILE] FileInput: end of file '%s'\n", i.path))

	if i.dryRun {
		fmt.Printf("Records found: %v\nFiles processed: %v\nBytes processed: %v\nMax wait: %v\nMin wait: %v\nFirst wait: %v\nIt will take `%v` to replay at current speed.\nFound %v records with out of order timestamp\n",
			i.stats.Get("total_counter"),
			i.stats.Get("reader_count"),
			i.stats.Get("total_bytes"),
			i.stats.Get("max_wait"),
			i.stats.Get("min_wait"),
			i.stats.Get("first_wait"),
			time.Duration(i.stats.Get("total_wait").(*expvar.Int).Value()),
			i.stats.Get("negative_wait"),
		)
	}
}

// Close closes this plugin
func (i *FileInput) Close() error {
	defer i.mu.Unlock()
	i.mu.Lock()

	close(i.exit)
	for _, r := range i.readers {
		r.Close()
	}

	return nil
}
