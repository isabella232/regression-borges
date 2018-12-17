package borges

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	"gopkg.in/src-d/regression-core.v0"
)

type Pack struct {
	*regression.Executor
	test   bool
	binary string
	repo   string
	files  []os.FileInfo
}

func NewPack(binary, repo string) (*Pack, error) {
	return &Pack{
		Executor: new(regression.Executor),
		binary:   binary,
		repo:     repo,
	}, nil
}

func (p *Pack) Run() error {
	list, err := createList(p.repo)
	if err != nil {
		return err
	}
	defer os.Remove(list)

	dir, err := createTempDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	lArg := fmt.Sprintf("%s", list)
	dArg := fmt.Sprintf("--root-repositories-dir=%s", dir)
	tArg := fmt.Sprintf("--timeout=4h")

	executor, err := regression.NewExecutor(p.binary, "pack", dArg, tArg, lArg)
	if err != nil {
		return err
	}

	p.Executor = executor

	err = p.Executor.Run()
	if err != nil {
		return err
	}

	files, err := fileInfo(dir)
	if err != nil {
		return err
	}

	p.files = files
	p.test = true

	return nil
}

func (p *Pack) Files() ([]os.FileInfo, error) {
	if !p.Executed {
		return nil, regression.ErrNotRun
	}

	return p.files, nil
}

func (p *Pack) Result() (*PackResult, error) {
	var size int64

	for _, file := range p.files {
		size += file.Size()
	}

	rusage, err := p.Rusage()
	if err != nil {
		return nil, err
	}

	wall, err := p.Wall()
	if err != nil {
		return nil, err
	}

	packResult := &PackResult{
		Memory:   rusage.Maxrss * 1024,
		Wtime:    wall,
		Stime:    time.Duration(rusage.Stime.Nano()),
		Utime:    time.Duration(rusage.Utime.Nano()),
		Files:    p.files,
		FileSize: size,
	}

	return packResult, nil
}

type PackResult struct {
	Memory   int64 // bytes
	Wtime    time.Duration
	Stime    time.Duration
	Utime    time.Duration
	Files    []os.FileInfo
	FileSize int64 // bytes
}

type PackComparison struct {
	Memory   float64
	Wtime    float64
	Stime    float64
	Utime    float64
	FileSize float64
}

const (
	Memory   = "memory"
	Time     = "time"
	FileSize = "file_size"
)

func (p *PackResult) SaveAllCSV(prefix string) error {
	for _, s := range []string{Memory, Time, FileSize} {
		if err := p.SaveCSV(s, fmt.Sprintf("%s%s.csv", prefix, s)); err != nil {
			return err
		}
	}

	return nil
}

func (p *PackResult) SaveCSV(series string, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	if err := p.WriteCSV(series, f); err != nil {
		_ = f.Close()
		return err
	}

	return f.Close()
}

func (p *PackResult) WriteCSV(series string, w io.Writer) error {
	switch series {
	case Memory:
		_, err := fmt.Fprintf(w, "%s\n%f\n", Memory, toMiB(p.Memory))
		return err
	case Time:
		_, err := fmt.Fprintf(w, "Wtime,Stime,Utime\n%f,%f,%f\n",
			p.Wtime.Seconds(), p.Stime.Seconds(), p.Utime.Seconds())
		return err
	case FileSize:
		_, err := fmt.Fprintf(w, "%s\n%f\n", FileSize, toMiB(p.FileSize))
		return err
	default:
		return fmt.Errorf("unsupported series: %s", series)
	}
}

func (p *PackResult) Compare(q *PackResult) PackComparison {
	return PackComparison{
		Memory:   percent(p.Memory, q.Memory),
		Wtime:    percent(int64(p.Wtime), int64(q.Wtime)),
		Stime:    percent(int64(p.Stime), int64(q.Stime)),
		Utime:    percent(int64(p.Utime), int64(q.Utime)),
		FileSize: percent(p.FileSize, q.FileSize),
	}
}

const (
	compareFormat = "%s: %v -> %v (%v), %v\n"
)

func toMiB(i int64) float64 {
	return float64(i) / float64(1024*1024)
}

func (p *PackResult) ComparePrint(q *PackResult, allowance float64) bool {
	ok := true
	c := p.Compare(q)

	if c.Memory > allowance {
		ok = false
	}
	fmt.Printf(compareFormat,
		"Memory",
		toMiB(p.Memory),
		toMiB(q.Memory),
		c.Memory,
		allowance > c.Memory,
	)

	if c.Wtime > allowance {
		ok = false
	}
	fmt.Printf(compareFormat,
		"Wtime",
		p.Wtime,
		q.Wtime,
		c.Wtime,
		allowance > c.Wtime,
	)

	fmt.Printf(compareFormat,
		"Stime",
		p.Stime,
		q.Stime,
		c.Stime,
		allowance > c.Stime,
	)

	fmt.Printf(compareFormat,
		"Utime",
		p.Utime,
		q.Utime,
		c.Utime,
		allowance > c.Utime,
	)

	if c.FileSize > allowance {
		ok = false
	}
	fmt.Printf(compareFormat,
		"FileSize",
		toMiB(p.FileSize),
		toMiB(q.FileSize),
		c.FileSize,
		allowance > c.FileSize,
	)

	return ok
}

func createList(repo string) (string, error) {
	tmpFile, err := ioutil.TempFile("", "packer-list")
	if err != nil {
		return "", err
	}

	_, err = tmpFile.WriteString(repo)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", err
	}

	err = tmpFile.Close()
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

func createTempDir() (string, error) {
	dir, err := ioutil.TempDir("", "packer-dir")
	if err != nil {
		return "", err
	}

	return dir, nil
}

func fileInfo(dir string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(dir)
}

func percent(a, b int64) float64 {
	diff := b - a
	return (float64(diff) / float64(a)) * 100
}
