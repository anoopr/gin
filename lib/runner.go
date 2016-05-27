package gin

import (
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

type Runner interface {
	Run() (*exec.Cmd, error)
	Info() (os.FileInfo, error)
	SetWriter(io.Writer)
	Kill() error
}

type runner struct {
	bin       string
	args      []string
	writer    io.Writer
	command   *exec.Cmd
	starttime time.Time
	status    string
	mux       sync.Mutex
}

func NewRunner(bin string, args ...string) Runner {
	return &runner{
		bin:       bin,
		args:      args,
		writer:    ioutil.Discard,
		starttime: time.Now(),
		status:    "new",
	}
}

func (r *runner) Run() (*exec.Cmd, error) {
	if r.needsRefresh() {
		r.Kill()
	}

	r.mux.Lock()
	defer r.mux.Unlock()

	for r.status == "starting" {
		time.Sleep(250 * time.Millisecond)
	}

	if r.command == nil || r.Exited() {
		err := r.runBin()
		return r.command, err
	} else {
		return r.command, nil
	}

}

func (r *runner) Info() (os.FileInfo, error) {
	return os.Stat(r.bin)
}

func (r *runner) SetWriter(writer io.Writer) {
	r.writer = writer
}

func (r *runner) Kill() error {
	if r.command != nil && r.command.Process != nil {
		done := make(chan error)
		go func() {
			r.command.Wait()
			close(done)
		}()

		//Trying a "soft" kill first
		if runtime.GOOS == "windows" {
			if err := r.command.Process.Kill(); err != nil {
				return err
			}
		} else if err := r.command.Process.Signal(os.Interrupt); err != nil {
			return err
		}

		//Wait for our process to die before we return or hard kill after 3 sec
		select {
		case <-time.After(3 * time.Second):
			if err := r.command.Process.Kill(); err != nil {
				log.Println("failed to kill: ", err)
			}
		case <-done:
		}
		r.command = nil
	}

	return nil
}

func (r *runner) Exited() bool {
	return r.command != nil && r.command.ProcessState != nil && r.command.ProcessState.Exited()
}

func (r *runner) runBin() error {
	r.status = "starting"
	r.command = exec.Command(r.bin, r.args...)
	log.Printf("First: %+v", r)
	stdout, err := r.command.StdoutPipe()
	if err != nil {
		r.status = "error"
		return err
	}
	stderr, err := r.command.StderrPipe()
	if err != nil {
		r.status = "error"
		return err
	}

	err = r.command.Start()
	if err != nil {
		r.status = "error"
		return err
	}

	r.starttime = time.Now()

	go io.Copy(r.writer, stdout)
	go io.Copy(r.writer, stderr)
	log.Printf("Second: %+v", r)
	go r.command.Wait()

	time.Sleep(1000 * time.Millisecond)
	r.status = "up"

	return nil
}

func (r *runner) needsRefresh() bool {
	info, err := r.Info()
	if err != nil {
		return false
	} else {
		return info.ModTime().After(r.starttime)
	}
}
