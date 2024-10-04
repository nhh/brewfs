package main

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed assets/caddy
var caddy []byte

type FS struct{}

func (FS) Root() (fs.Node, error) {
	return Dir{}, nil
}

type Dir struct{}

func (Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Mode = os.ModeDir | 0544
	return nil
}

// Lookup: Sucht nach einer Datei im Verzeichnis
func (d Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	if name == "caddy" {
		return &File{inode: 3, content: caddy}, nil
	}
	return nil, fuse.ENOENT
}

// Todo hold a list of files and return them here
func (d Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	return []fuse.Dirent{
		{Inode: 3, Name: "caddy", Type: fuse.DT_File},
	}, nil
}

// Datei Node
type File struct {
	inode   uint64
	content []byte
}

// Datei-Attribute (jedes File hat eine eindeutige Inode)
func (f File) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = f.inode
	a.Mode = 0544 // Besitzer read and exec
	a.Size = uint64(len(f.content))
	return nil
}

// Lesen aus der Datei
func (f File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	data := f.content
	if req.Offset >= int64(len(data)) {
		return nil // Nichts mehr zu lesen
	}

	// Berechne, wie viele Bytes wir lesen können
	resp.Data = data[req.Offset:]
	if size := req.Size; size < len(resp.Data) {
		resp.Data = resp.Data[:size]
	}

	return nil
}

func main() {
	home, err := os.UserHomeDir()

	mountpoint := path.Join(home, "bin")

	// Mounten des Dateisystems
	c, err := fuse.Mount(mountpoint)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		killAllServices(mountpoint)
		fuse.Unmount(mountpoint)
		fmt.Println(sig)
	}()

	// Bereitstellung des Dateisystems
	if err := fs.Serve(c, FS{}); err != nil {
		log.Fatal(err)
	}
}

func killAllServices(mountPoint string) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		log.Fatal(err)
	}

	for _, pid := range entries {
		p := path.Join("/proc", pid.Name(), "exe")

		_, err := os.Stat(p)

		if err != nil {
			continue
		}

		absPathm, err := filepath.EvalSymlinks(p)

		if err != nil {
			log.Println(err)
		}

		if !strings.Contains(absPathm, mountPoint) {
			continue
		}

		n, err := strconv.Atoi(pid.Name())

		if err != nil {
			log.Println(err)
		}

		proc, err := os.FindProcess(n)
		if err != nil {
			log.Println(err)
		}
		// Kill the process
		proc.Signal(os.Interrupt)

		for {
			if pidExists(n) {
				continue
			}
			time.Sleep(16 * time.Millisecond)
			break
		}

		fmt.Printf("Killed process: %d\n", n)
	}
}

func pidExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if err.Error() == "os: process already finished" {
		return false
	}
	errno, ok := err.(syscall.Errno)
	if !ok {
		return false
	}
	switch errno {
	case syscall.ESRCH:
		return false
	case syscall.EPERM:
		return true
	default:
		return false // Todo? correct?
	}
	return false
}
